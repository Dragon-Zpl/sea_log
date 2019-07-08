package admin

import (
	"errors"
	"github.com/gin-gonic/gin"
	"sea_log/common"
	"sea_log/common/sealog_errors"
	"sea_log/master/balance"
	"sea_log/master/conf"
	"sea_log/master/etcd"
	"sea_log/master/utils"
	"strings"
	"encoding/json"
)

func Mapping(prefix string, app *gin.Engine) {
	admin := app.Group(prefix)
	admin.POST("/job", sealog_errors.MiddlewareError(AddLogJob))
	admin.DELETE("/job", sealog_errors.MiddlewareError(DelLogJob))
	admin.POST("bulk/job", sealog_errors.MiddlewareError(BulkAddLogJob))
	admin.DELETE("bulk/job", sealog_errors.MiddlewareError(BulkDelLogJob))
}

func AddLogJob(ctx *gin.Context) error {
	var jobs common.Jobs
	if ctx.ShouldBind(&jobs) != nil {
		return errors.New("params_error")
	}

	runJobs := etcd.GetAllRuningJob()
	if ip, ok := runJobs[jobs.JobName]; ok { // 更新job
		err := etcd.DistributeJob(ip, jobs)
		if err != nil {
			return errors.New("distributeJob_error")
		}
	} else {
		if ip, err := balance.BlanceMapping[conf.BalanceConf.Name]().GetRightNode(); err == nil {
			err := etcd.DistributeJob(ip, jobs)
			if err != nil {
				return errors.New("distributeJob_error")
			}
		}else {
			return errors.New("get_nodes_fail")
		}
	}
	ctx.JSON(utils.Success())
	return nil
}

func DelLogJob(ctx *gin.Context) error {

	return nil
}

type AddBulkStrings struct {
	Jobs string `form:"jobs" json:"jobs" binding:"required"`
}

func BulkAddLogJob(ctx *gin.Context) error {
	var addBulkStrings AddBulkStrings
	var jobs common.Jobs
	if err := ctx.ShouldBind(&addBulkStrings); err != nil{
		return err
	}else {
		data_slice := strings.Split(addBulkStrings.Jobs,";")
		runJobs := etcd.GetAllRuningJob()
		for i := range data_slice{
				if err := json.Unmarshal([]byte(data_slice[i]),&jobs); err != nil{
					return errors.New("params_error")
				}else{
					if ip, ok := runJobs[jobs.JobName]; ok { // 更新job
						etcd.DistributeJob(ip, jobs)
					}else {
						if ip, err := balance.BlanceMapping[conf.BalanceConf.Name]().GetRightNode(); err == nil{
							etcd.DistributeJob(ip,jobs)
						}else {
							return err
						}
					}
				}
			}
		ctx.JSON(utils.Success())
	}
	return nil
}

type DeleteBulkStrings struct {
	JobNames string `form:"jobnames" json:"jobnames" binding:"required"`
}

func BulkDelLogJob(ctx *gin.Context) error {
	var deleteBulkStrings DeleteBulkStrings
	if err := ctx.ShouldBind(&deleteBulkStrings); err != nil{
		return errors.New("params_error")
	}else {
		data_slice := strings.Split(deleteBulkStrings.JobNames,",")
		jobNodeInfo := etcd.GetAllRuningJob()
		for i := range data_slice{
			if ip, ok := jobNodeInfo[data_slice[i]]; ok{
				etcd.UnDistributeJob(ip, data_slice[i])
			}
		}
		ctx.JSON(utils.Success())
	}
	return nil
}
