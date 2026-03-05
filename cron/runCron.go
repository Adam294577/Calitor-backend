package cron

import (
	"project/services/log"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/spf13/viper"
)

var cronjob *cron.Cron
var cronMutex sync.Mutex

func Run() {
	cronMutex.Lock()
	defer cronMutex.Unlock()

	// 如果已經有跑的，先停掉
	if cronjob != nil {
		ctx := cronjob.Stop()
		<-ctx.Done() // 等待所有 goroutine 結束
		log.Info("Old cron stopped")
	}

	c := cron.New(cron.WithSeconds())
	//			  ┌─────────────── 秒     (0 - 59)
	//            | ┌───────────── 分鐘   (0 - 59)
	//            | │ ┌─────────── 小時   (0 - 23)
	//            | │ │ ┌───────── 日     (1 - 31)
	//            | │ │ │ ┌─────── 月     (1 - 12)
	//            | │ │ │ │ ┌───── 星期幾 (0 - 6，0 是週日，6 是週六，7 也是週日)
	//			  │ │ │ │ │ │
	// c.AddFunc("5 * * * * *", func())
	ctx := map[string]error{}
	if viper.GetString("ENV") == "prod" {

	}

	if len(ctx) != 0 {
		log.Debug("cron run error: %v", ctx)
	}
	if len(c.Entries()) > 0 {
		c.Start()
		log.Info("The Cron Jobs Running")
	} else {
		log.Info("No Cron Entries")
	}

	cronjob = c
}
