package kafka

import (
	"github.com/Shopify/sarama"
	"log"
	"sea_log/logs"
	"sea_log/slaver/conf"
	"sea_log/slaver/es"
	"sea_log/slaver/etcd_ops"
	"sea_log/slaver/utils"
	"strings"
	"time"
	//"sync"
	//"time"
)

func SendToKafka(datas []string, topic string) error {
	var (
		msgs []*sarama.ProducerMessage
		err  error
	)
	msgs = make([]*sarama.ProducerMessage, 0)
	for _, data := range datas {
		msgs = append(msgs, &sarama.ProducerMessage{
			Topic: topic,
			Value: sarama.ByteEncoder(data),
		})
	}
	err = Producer.SendMessages(msgs)
	if err != nil {
		logs.ERROR(err)
	}
	return err
}

func ConsumerFromKafka(info *utils.JobWorkInfo, lock *etcd_ops.JobLock, logCount chan int) {
	config := sarama.NewConfig()
	config.Version = sarama.V2_0_1_0
	client, err := sarama.NewClient([]string{conf.KafkaConf.Addr}, config)
	if err != nil {
		log.Fatalln(err)
	}

	offsetManager, err := sarama.NewOffsetManagerFromClient("test", client)
	if err != nil {
		log.Fatalln(err)
	}

	pids, err := client.Partitions(info.Job.Topic)
	if err != nil {
		log.Fatalln(err)
	}

	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		log.Fatalln(err)
	}

	defer consumer.Close()

	//wg := &sync.WaitGroup{}

	for _, v := range pids {
		//wg.Add(1)
		go consume(consumer, offsetManager, v, info, lock, logCount)
	}
}
func consume(c sarama.Consumer, om sarama.OffsetManager, p int32, info *utils.JobWorkInfo, lock *etcd_ops.JobLock, logCount chan int) {
	var (
		pom    sarama.PartitionOffsetManager
		pc     sarama.PartitionConsumer
		err    error
		offset int64
	)
	defer c.Close()

	if pom, err = om.ManagePartition(info.Job.Topic, p); err != nil {
		log.Fatalln(err)
	}

	defer pom.Close()

	offset, _ = pom.NextOffset()
	if offset == -1 {
		offset = sarama.OffsetOldest
	}

	if pc, err = c.ConsumePartition(info.Job.Topic, p, offset); err != nil {
		log.Fatalln(err)
	}

	defer pc.Close()
	var (
		logDatas []interface{}
		t        *time.Timer
	)
	logDatas = make([]interface{}, 0)
	t = time.NewTimer(time.Second * 1)
	for {
		select {
		case msg := <-pc.Messages():
			logDatas = append(logDatas, string(msg.Value))

			pom.MarkOffset(msg.Offset+1, "")
			//es.GelasticCli.CreateSignDocument(utils.CreateIndexByType(info.Job.Topic, info.Job.IndexType), string(msg.Value), info.Job.Pipeline)
			if len(logDatas) >= 1000 {
				es.GelasticCli.CreateBulkDocument(strings.ToLower(utils.CreateIndexByType(info.Job.Topic, info.Job.IndexType)), logDatas, info.Job.Pipeline)

				logCount <- len(logDatas)
				utils.SliceClear(&logDatas)
			}
			//log.Printf("[%v] Consumed message offset %v content is %s\n", p, msg.Offset, string(msg.Value))
		case <-t.C:
			if len(logDatas) > 0 {
				es.GelasticCli.CreateBulkDocument(strings.ToLower(utils.CreateIndexByType(info.Job.Topic, info.Job.IndexType)), logDatas, info.Job.Pipeline)

				logCount <- len(logDatas)
				utils.SliceClear(&logDatas)
			}
			t.Reset(time.Second * 1)
		case <-info.ConText.Done():
			lock.Unlock()
			return
		}
	}
	//for msg := range pc.Messages() {
	//	log.Printf("[%v] Consumed message offset %v content is %s\n", p, msg.Offset, string(msg.Value))
	//	pom.MarkOffset(msg.Offset+1, "")
	//}
	logs.INFO("退出" + info.Job.Topic + "日志收集协程")
}
