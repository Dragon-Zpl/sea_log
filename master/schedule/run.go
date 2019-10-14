package schedule

import (
	"context"
	"github.com/coreos/etcd/clientv3"
	"sea_log/common"
	"sea_log/master/balance"
	"sea_log/master/conf"
	"sea_log/master/etcd"
	"strings"
	"time"
)

type Scheduler struct {
	b      balance.Balancer
	jobRun chan common.Jobs
	jobEnd chan string
}

func newScheduler(b balance.Balancer) *Scheduler {
	return &Scheduler{
		b:      b,
		jobRun: make(chan common.Jobs, 10),
		jobEnd: make(chan string, 10),
	}
}

func InitSchedule() {
	s := newScheduler(balance.BlanceMapping[conf.BalanceConf.Name]())
	go s.ListenJob()
	go s.StartLogJob()
	go s.EndLogJob()
	go s.restartLogJob()
}

func (this *Scheduler) ListenJob() {
	getResp, err := etcd.EtcdClient.KV.Get(context.TODO(), conf.MasterConf.Jobs, clientv3.WithPrefix())
	if err != nil {
		panic(err)
	}

	// 启动时先将etcd中的 job 消费
	allRunJob := etcd.GetAllRuningJob()
	for _, v := range getResp.Kvs {
		if job, err := common.UnPackJob(v.Value); err == nil {
			if _, ok := allRunJob[job.JobName]; !ok {
				this.jobRun <- *job
			}
		}
	}
	go func() {
		// 从getResp header 下一个版本进行监听
		watcherReversion := getResp.Header.Revision + 1
		watchChan := etcd.EtcdWatch.Watch(context.Background(), conf.MasterConf.Jobs, clientv3.WithPrefix(), clientv3.WithRev(watcherReversion))
		for eachChan := range watchChan {
			for _, v := range eachChan.Events {
				switch v.Type {
				case clientv3.EventTypePut:
					if job, err := common.UnPackJob(v.Kv.Value); err == nil {
						this.jobRun <- *job
					}
				case clientv3.EventTypeDelete:
					this.jobEnd <- string(v.Kv.Key)
				}
			}
		}
	}()
}

func (this *Scheduler) StartLogJob() {
	var job common.Jobs
	for {
		select {
		case job = <-this.jobRun:
			runJobs := etcd.GetAllRuningJob()
			if ip, ok := runJobs[job.JobName]; ok { // 更新job
				etcd.DistributeJob(ip, job)
			} else {
				if ip, err := this.b.GetRightNode(); err == nil {
					etcd.DistributeJob(ip, job)
				}
			}
		}
	}
}

func (this *Scheduler) EndLogJob() {
	var jobName string
	for {
		select {
		case jobName = <-this.jobEnd:
			jobNodeInfo := etcd.GetAllRuningJob()
			if ip, ok := jobNodeInfo[jobName]; ok {
				etcd.UnDistributeJob(ip, jobName)
			}
		}
	}
}

// 容灾处理
func (this *Scheduler) restartLogJob() {
	t := time.NewTimer(time.Minute)
	for {
		select {
		case <-t.C:
			allJobs := etcd.GetAllJob()
			allRunJob := etcd.GetAllDistributeJob()
			if len(allRunJob) > len(allJobs) {
				panic("running job gt all job")
			}
			for jobName, job := range allJobs {
				//去掉/master/job前缀
				jobName = strings.Replace(jobName, conf.MasterConf.Jobs, "", -1)
				if _, ok := allRunJob[jobName]; !ok {
					this.jobRun <- *job
				}
			}
			t.Reset(time.Minute)
		}
	}
}
