package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/coreos/etcd/clientv3"
	"golang.org/x/net/context"
	"sea_log/common"
	"sea_log/etcd"
	"sea_log/logs"
	"sea_log/slaver/conf"
	"sea_log/slaver/etcd_ops"
	"sea_log/slaver/kafka"
	"sea_log/slaver/utils"
	"time"
)

func InitScheduler() {
	NewScheduler()
	//Gscheduler.Register()
	go Gscheduler.ScheuleLoop()
	Gscheduler.restartLogJob()
	go Gscheduler.CalculatingPressure()
	WatcherJobs()
}

// 注销所有信息
func CancelSelf() {
	Gscheduler.HeartCancelFunc()
	etcd_ops.GjobMgr.Kv.Delete(context.Background(), conf.JobConf.JobInfo, clientv3.WithPrefix())
	etcd_ops.GjobMgr.Kv.Delete(context.Background(), conf.JobConf.JobLock, clientv3.WithPrefix())
	etcd_ops.GjobMgr.Kv.Delete(context.Background(), conf.JobConf.JobSave, clientv3.WithPrefix())
}

func NewScheduler() {
	Gscheduler = &Scheduler{
		logCount:     make(chan int, 1000),
		JobEventChan: make(chan *utils.JobEvent, 1000),
		JobWorkTable: make(map[string]*utils.JobWorkInfo),
	}
}

//func (this Scheduler) Register() {
//	if err := etcd.GjobMgr.CreateJobLock(conf.JobConf.JobSave).TryToLock(); err != nil {
//		panic(fmt.Sprintf("该 node  register err: %v", err))
//	}
//
//}

// push log任务事件
func (this *Scheduler) PushJobEvent(event *utils.JobEvent) {
	this.JobEventChan <- event
}

func (this *Scheduler) ScheuleLoop() {
	var (
		jobEvent *utils.JobEvent
	)
	for {
		select {
		case jobEvent = <-this.JobEventChan:
			this.handleJobEvent(jobEvent)
		}
	}
}

//开启topic 的kafka消费
func (this *Scheduler) eventWorker(job *common.Jobs) {
	var (
		jobWorkInfo *utils.JobWorkInfo
		jobLock     *etcd_ops.JobLock
		err         error
	)
	jobLock = etcd_ops.GjobMgr.CreateJobLock(job.JobName)
	err = jobLock.TryToLock()
	//defer jobLock.Unlock()
	if err == nil {
		jobBytes, err := common.PackJob(*job)
		if err != nil {
			logs.ERROR(err)
			return
		}
		//创建租约
		leaseId, leaseKeepActiveChan, _, _, err := etcd.CreateLeaseAndKeepAlive(etcd_ops.GjobMgr.Lease, 5)
		if err != nil {
			return
		}
		etcd_ops.GjobMgr.Kv.Put(context.Background(), conf.JobConf.JobSave+job.JobName, string(jobBytes), clientv3.WithLease(leaseId))

		jobWorkInfo = utils.NewJobWorkInfo(job)
		if jobWork, ok := this.JobWorkTable[job.Topic]; !ok {
			this.JobWorkTable[job.Topic] = jobWorkInfo
			kafka.ConsumerFromKafka(jobWorkInfo, jobLock, this.logCount)
		} else {
			// 重新开启新任务
			this.reEventWork(jobWork, job, jobLock)
		}

		//进行监听 cancelfunc
		go func() {
			var (
				leaseKeepResp *clientv3.LeaseKeepAliveResponse
			)
			for {
				select {
				case leaseKeepResp = <-leaseKeepActiveChan: // 租约消失
					if leaseKeepResp == nil {
						goto END
					}
				}
			}
		END:
		}()
	}
}

// 更新任务
func (this *Scheduler) reEventWork(jobWork *utils.JobWorkInfo, newJob *common.Jobs, lock *etcd_ops.JobLock) {
	var (
		jobWorkInfo *utils.JobWorkInfo
	)
	// 先关闭当前任务
	jobWork.CancelFunc()
	delete(this.JobWorkTable, jobWork.Job.Topic)
	jobWorkInfo = utils.NewJobWorkInfo(newJob)
	this.JobWorkTable[newJob.Topic] = jobWorkInfo
	kafka.ConsumerFromKafka(jobWorkInfo, lock, this.logCount)
}

// 处理日志任务
func (this *Scheduler) handleJobEvent(event *utils.JobEvent) {
	switch event.EventType {
	case utils.JOB_EVENT_SAVE:
		this.eventWorker(event.Job)
	case utils.JOB_EVENT_DELETE:
		if jobWork, ok := this.JobWorkTable[event.Job.Topic]; ok {
			jobWork.CancelFunc()
			delete(this.JobWorkTable, jobWork.Job.Topic)
		}
	}
}

// 进行监听log任务
func WatcherJobs() {
	var (
		getResp          *clientv3.GetResponse
		err              error
		job              *common.Jobs
		jobEvent         *utils.JobEvent
		watcherReversion int64
		watchChan        clientv3.WatchChan
	)
	if getResp, err = etcd_ops.GjobMgr.Kv.Get(context.TODO(), conf.JobConf.JobSave, clientv3.WithPrefix()); err != nil {
		logs.ERROR(err)
		return
	}
	// 启动时先将etcd中的topic消费
	for _, v := range getResp.Kvs {
		if job, err = common.UnPackJob(v.Value); err == nil {
			jobEvent = utils.BuildJobEvent(utils.JOB_EVENT_SAVE, job)
			Gscheduler.PushJobEvent(jobEvent)
		}
	}
	go func() {
		// 从getResp header 下一个版本进行监听
		watcherReversion = getResp.Header.Revision + 1
		watchChan = etcd_ops.GjobMgr.Watcher.Watch(context.TODO(), conf.JobConf.JobSave, clientv3.WithRev(watcherReversion), clientv3.WithPrefix())
		for eachChan := range watchChan {
			for _, v := range eachChan.Events {
				switch v.Type {
				// 新的任务
				case clientv3.EventTypePut:
					if job, err = common.UnPackJob(v.Kv.Value); err == nil {
						jobEvent = utils.BuildJobEvent(utils.JOB_EVENT_SAVE, job)
						Gscheduler.PushJobEvent(jobEvent)
					}
					// 删除任务
				case clientv3.EventTypeDelete:
					jobEvent = utils.BuildJobEvent(utils.JOB_EVENT_DELETE, &common.Jobs{
						Topic: utils.ExtractJobName(string(v.Kv.Key)),
					})
					Gscheduler.PushJobEvent(jobEvent)
				}
			}
		}
	}()
}

// 容灾处理
func (this *Scheduler) restartLogJob() {
	var (
		t *time.Timer
	)
	t = time.NewTimer(time.Second * 60)
	go func() {
		for {
			select {
			case <-t.C:
				var (
					jobs  []*common.Jobs
					locks map[string]string
					err   error
				)
				// 所有在etcd中的log任务
				if jobs, err = etcd_ops.GjobMgr.ListLogJobs(); err != nil {
					logs.ERROR(err)
					return
				}
				// 所有抢到锁的任务
				if locks, err = etcd_ops.GjobMgr.ListLogLocks(); err != nil {
					logs.ERROR(err)
					return
				}
				for _, job := range jobs {
					if _, ok := locks[job.Topic]; !ok {
						this.PushJobEvent(&utils.JobEvent{
							EventType: utils.JOB_EVENT_SAVE,
							Job:       job,
						})
					}
				}
				t.Reset(time.Second * 60)
			}
		}
	}()
}

// 计算压力和心跳
func (this *Scheduler) CalculatingPressure() {
	var (
		nodeInfo common.NodeInfo
		counts   []int
	)
	//  用做信息上报和心跳
	leaseId, leaseKeepActiveChan, _, cancelFunc, err := etcd.CreateLeaseAndKeepAlive(etcd_ops.GjobMgr.Lease, 10)
	if err != nil {
		panic(fmt.Sprintf("CalculatingPressure CreateLease err: %v", err))
	}
	this.HeartCancelFunc = cancelFunc
	t := time.NewTimer(time.Second * 10)

	//进行监听 cancelfunc
	go func() {
		var (
			leaseKeepResp *clientv3.LeaseKeepAliveResponse
		)
		for {
			select {
			case leaseKeepResp = <-leaseKeepActiveChan: // 租约消失
				if leaseKeepResp == nil {
					goto END
				}
			}
		}
	END:
	}()

	for {
		select {
		case count := <-this.logCount:
			counts = append(counts, count)
		case <-t.C:
			for _, count := range counts {
				nodeInfo.LogCount += int64(count)
			}

			// 上报信息
			nodeInfo.JobCount = len(this.JobWorkTable)
			infoBytes, _ := json.Marshal(&nodeInfo)
			etcd_ops.GjobMgr.Kv.Put(context.Background(), conf.JobConf.JobInfo, string(infoBytes), clientv3.WithLease(leaseId))
			logs.INFO("node info is ", nodeInfo)

			// 任务数会变为0， 零时改动再做修改
			if nodeInfo.JobCount == 0 && nodeInfo.LogCount == 0{
				logs.INFO("重新初始化")
				InitScheduler()
				time.Sleep(30*time.Second)
			}
			counts = []int{}
			nodeInfo = common.NodeInfo{}

			t.Reset(time.Second * 10)
		}
	}
}
