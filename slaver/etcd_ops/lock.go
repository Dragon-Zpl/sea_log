package etcd_ops

import (
	"context"
	"errors"
	"github.com/coreos/etcd/clientv3"
	"sea_log/etcd"
	"sea_log/logs"
	"sea_log/slaver/conf"
)

//分布式乐观锁

func InitJobLock(jobName string, kv clientv3.KV, lease clientv3.Lease) *JobLock {
	return &JobLock{
		jobName: jobName,
		kv:      kv,
		lease:   lease,
	}
}

func (this *JobLock) TryToLock() error {
	var (
		leaseId             clientv3.LeaseID
		leaseKeepActiveChan <-chan *clientv3.LeaseKeepAliveResponse
		//ctx                 context.Context
		cancelFunc context.CancelFunc
		txn        clientv3.Txn
		lockKey    string
		txnResp    *clientv3.TxnResponse
		err        error
	)
	// 创建一个租约
	if leaseId, leaseKeepActiveChan, _, cancelFunc, err = etcd.CreateLeaseAndKeepAlive(this.lease, 10); err != nil {
		logs.ERROR(err)
		goto FAIL
	}
	// 进行续租
	//if leaseKeepActiveChan, err = this.lease.KeepAlive(ctx, leaseId); err != nil {
	//	logs.ERROR(err)
	//	goto FAIL
	//}

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
	// 锁路径
	lockKey = conf.JobConf.JobLock + this.jobName

	// 创建一个事物
	txn = this.kv.Txn(context.TODO())

	// 抢锁
	txn.If(clientv3.Compare(clientv3.CreateRevision(lockKey), "=", 0)).
		Then(clientv3.OpPut(lockKey, "lock", clientv3.WithLease(leaseId))).
		Else(clientv3.OpGet(lockKey))

	//提交事物
	if txnResp, err = txn.Commit(); err != nil {
		goto FAIL
	}

	if !txnResp.Succeeded {
		err = errors.New("锁正在被占用")
		logs.INFO(this.jobName + " 锁被占用")
		goto FAIL
	}

	//抢锁成功
	this.leaseId = leaseId
	this.cancelFunc = cancelFunc
	this.isLocked = true
	return err

FAIL:
	cancelFunc()                               // 取消自动续租
	this.lease.Revoke(context.TODO(), leaseId) //  释放租约
	return err
}

// 释放锁
func (this *JobLock) Unlock() {
	if this.isLocked {
		this.cancelFunc()                               // 取消我们程序自动续租的协程
		this.lease.Revoke(context.TODO(), this.leaseId) // 释放租约
	}
}
