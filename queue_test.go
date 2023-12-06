package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"

	//"math/big"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cmap "github.com/orcaman/concurrent-map"
)

var (
	mtx          sync.RWMutex
	mtxComein    sync.RWMutex
	recordForBle cmap.ConcurrentMap
	LimitTimes   int
	Counter      int
)

type InfoMssage struct {
	frame   int64
	message string
	gw      string
}

func resend(frame, resendTimes int64, q1 *BoundedQueue[InfoMssage], key string) {
	if resendTimes == 4 {
		fmt.Println(key, "已重发次数达到上限，表征当前设备存在问题")
		_, ok := recordForBle.Get(key)
		if ok {
			recordForBle.Remove(key)
		}
		return
	}
	timer := time.NewTimer((time.Second * time.Duration(30)))
	defer timer.Stop()
	<-timer.C
	if q1.Len() > 0 && frame == q1.q.Peek().frame {
		fmt.Println("重发消息了")
		s1 := strconv.Itoa(int(frame)) + ":" + key
		ch <- s1
		resend(frame, resendTimes+1, q1, key)
	}
}

// 模拟消息的生产------消费模式
// 仅内存情况
// 考虑主动下发的时候作为 消息生产者  ----> 消息map（网关+ 插卡作为唯一标识） || 蓝牙mac作为唯一
// 从udpserver服务回来的时候直接从对应地方去取接口，不用在后面跟着去进行判断
// 鉴于可能对多个网关发送消息，所以，创建缓存时需要加锁
// 过时键清除策略，1小时---1天等方式进行定时删除，队列消息为空的缓存键
var ch chan string

// 收到消息是不带锁的， 模拟过程必须带锁
func TestMain(t *testing.T) {
	var wg sync.WaitGroup
	LimitTimes = 4
	recordForBle = cmap.New()      
	recordForBleFrame := cmap.New() 
	bleMacArrays := []string{"1", "2", "3", "4"} 
	ch = make(chan string)
	wg.Add(1)
	go func() { //模拟生产者，在这个过程中，如果
		timer := time.NewTimer(10 * time.Second)
		for {
			select {
			case <-timer.C:
				return
			default:
				for i := 0; i < LimitTimes; i++ { 				//模拟server侧主动给生成的消息 模拟不同网关加插卡出去的信息
					go func(wg int) {
						updata := make([]byte, 20)
						msg := InfoMssage{}
						mac := make([]byte, 1)
						rand.Read(mac)
						rand.Read(updata)
						mtx.Lock()
						recordForBleFrame.SetIfAbsent(bleMacArrays[wg], int64(0))
						curGwFrame, _ := recordForBleFrame.Get(bleMacArrays[wg])
						recordForBleFrame.Set(bleMacArrays[wg], curGwFrame.(int64)+1)
						//Counter++
						msg.frame = curGwFrame.(int64) + 1
						//fmt.Println(msg.frame)
						mtx.Unlock()
						msg.message = hex.EncodeToString(updata)
						msg.gw = bleMacArrays[wg]
						recordForBle.SetIfAbsent(bleMacArrays[wg], NewBoundedQueue[InfoMssage](1500))
						pq, _ := recordForBle.Get(bleMacArrays[wg])
						fmt.Println("网关地址:", bleMacArrays[wg], "消息信息:", msg.message, "当前帧:", msg.frame, "时间：", time.Now())
						pq.(*BoundedQueue[InfoMssage]).Enqueue(msg)
						go func(mg InfoMssage, q1 *BoundedQueue[InfoMssage]) {
							//fmt.Println("理论上进来")
							for q1.q.Peek().frame != mg.frame {
								fmt.Println("网关地址:", mg.gw, "尚未发送消息:", mg.frame, "时间：", time.Now())
								time.Sleep(time.Second)
							}
							ch <- strconv.Itoa(int(mg.frame)) + ":" + mg.gw //通知一下对端消费者可以开始消费，下发消息
							go resend(mg.frame, 1, q1, mg.gw)
						}(msg, pq.(*BoundedQueue[InfoMssage]))
					}(i % 4)
				}
				time.Sleep(time.Millisecond * 200)
			}
		}
	}()

	// 收到消息的情况下，随意模拟当前获得的网关参数，当然
	// go func() {
	// 	time.Sleep(2 * time.Second)
	// 	//模拟消息处理响应时长
	// 	for Counter > 0 {
	// 		for i := 0; i < 4; i++ {
	// 			go func(wg int) { //接收到来自对应网关+插卡的的消息时, 从对应消息队列里查看,循环仅仅是为了模仿网关进入的前后情况
	// 				mtxComein.Lock()
	// 				recordForBleFrameComein.SetIfAbsent(bleMacArrays[wg], 0)
	// 				comeInGwFrame, _ := recordForBleFrame.Get(bleMacArrays[wg])
	// 				recordForBleFrameComein.Set(bleMacArrays[wg], comeInGwFrame.(int64)+1)
	// 				Counter--
	// 				mtxComein.Unlock()
	// 				pq, ok := recordForBle.Get(bleMacArrays[wg])
	// 				if !ok || pq.(*BoundedQueue[InfoMssage]).len == 0 { //错误值
	// 					fmt.Println("当前队列无消息，放弃处理")
	// 					return
	// 				}
	// 				ackMessage := pq.(*BoundedQueue[InfoMssage]).Dequeue()
	// 				fmt.Println("确认网关消息号为：", bleMacArrays[wg], "确认网关的消息为：", ackMessage.message, "当前帧为：", ackMessage.frame)
	// 			}(i % 4)
	// 			time.Sleep(time.Millisecond * 50)
	// 		}
	// 	}
	// }()

	for inbound := range ch {
		inboundMsg := strings.Split(inbound, ":") //不要再for range后面直接写异步操作，可能传入多组参数是相同的
		frame, _ := strconv.Atoi(inboundMsg[0])
		gw := inboundMsg[1]
		pq, has := recordForBle.Get(gw)
		p1 := pq.(*BoundedQueue[InfoMssage])
		fmt.Println("开始处理网关:", gw, "消息为帧数：", frame, "时间", time.Now())
		go func(frame int, qq *BoundedQueue[InfoMssage]) { //描述接受到新的udp信息
			randomDelay, _ := rand.Int(rand.Reader, big.NewInt(int64(100)+1))
			duration := time.Duration(randomDelay.Int64()) * time.Millisecond //模拟消息处理
			time.Sleep(duration)
			if has && p1.Len() != 0 {
				if p1.q.Peek().frame > int64(frame) {
					fmt.Println("过期消息，不予以理会")
					return
				} else if p1.q.Peek().frame < int64(frame) {
					fmt.Println("理论上不存在")
					return
				}
				ackMessage := pq.(*BoundedQueue[InfoMssage]).Dequeue()
				fmt.Println("确认网关消息号为：", ackMessage.gw, "确认网关的消息为：", ackMessage.message, "当前帧为：", ackMessage.frame, "时间：", time.Now())
			}
		}(frame, p1)
	}
	wg.Wait()
}

func TestXxx(t *testing.T) {
	queue := NewBoundedQueue[int](3)
	go func() {
		for i := 0; i < 5; i++ {
			queue.Enqueue(i)
			fmt.Println("Enqueued:", i)
		}
	}()

	go func() {
		for i := 0; i < 5; i++ {
			fmt.Println("Dequeued:", queue.Dequeue())
		}
	}()

	select {}
}
