package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IBM/sarama"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"websocket/socketio"
	socketws "websocket/socketio/websocket"
)

// MessageObject Basic chat message object
type MessageObject struct {
	Data  string `json:"data"`
	From  string `json:"from"`
	Event string `json:"event"`
	To    string `json:"to"`
}

var (
	brokers  = "172.16.3.88:6667"
	group    = "mlgd"
	topics   = "park_alarm"
	assignor = "roundrobin"
	oldest   = false
	verbose  = true
)

var instance *socketio.SocketInstance

// Kafka消费者定义抄
// Consumer represents a Sarama consumer group consumer
type Consumer struct {
	ready chan bool
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (consumer *Consumer) Setup(sarama.ConsumerGroupSession) error {
	// Mark the consumer as ready
	close(consumer.ready)
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (consumer *Consumer) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages().
// Once the Messages() channel is closed, the Handler must finish its processing
// loop and exit.
func (consumer *Consumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// NOTE:
	// Do not move the code below to a goroutine.
	// The `ConsumeClaim` itself is called within a goroutine, see:
	// https://github.com/IBM/sarama/blob/main/consumer_group.go#L27-L29
	for {
		select {
		case message, ok := <-claim.Messages():
			if !ok {
				log.Printf("message channel was closed")
				return nil
			}
			instance.Broadcast(message.Value, socketio.TextMessage)
			log.Printf("Message claimed: value = %s, timestamp = %v, topic = %s", string(message.Value), message.Timestamp, message.Topic)
			session.MarkMessage(message, "")
		// Should return when `session.Context()` is done.
		// If not, will raise `ErrRebalanceInProgress` or `read tcp <ip>:<port>: i/o timeout` when kafka rebalance. see:
		// https://github.com/IBM/sarama/issues/1192
		case <-session.Context().Done():
			return nil
		}
	}
}

func toggleConsumptionFlow(client sarama.ConsumerGroup, isPaused *bool) {
	if *isPaused {
		client.ResumeAll()
		log.Println("Resuming consumption")
	} else {
		client.PauseAll()
		log.Println("Pausing consumption")
	}

	*isPaused = !*isPaused
}

func ReceiveKafkaData() {
	keepRunning := true
	log.Println("Starting a new Sarama consumer")

	if verbose {
		sarama.Logger = log.New(os.Stdout, "[sarama] ", log.LstdFlags)
	}

	/**
	 * Construct a new Sarama configuration.
	 * The Kafka cluster version has to be defined before the consumer/producer is initialized.
	 */
	config := sarama.NewConfig()
	config.Version = sarama.V3_6_0_0

	switch assignor {
	case "sticky":
		config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategySticky()}
	case "roundrobin":
		config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	case "range":
		config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	default:
		log.Panicf("Unrecognized consumer group partition assignor: %s", assignor)
	}

	if oldest {
		config.Consumer.Offsets.Initial = sarama.OffsetOldest
	}

	/**
	 * Setup a new Sarama consumer group
	 */
	consumer := Consumer{
		ready: make(chan bool),
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := sarama.NewConsumerGroup(strings.Split(brokers, ","), group, config)
	if err != nil {
		log.Panicf("Error creating consumer group client: %v", err)
	}

	consumptionIsPaused := false
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			// `Consume` should be called inside an infinite loop, when a
			// server-side rebalance happens, the consumer session will need to be
			// recreated to get the new claims
			if err := client.Consume(ctx, strings.Split(topics, ","), &consumer); err != nil {
				if errors.Is(err, sarama.ErrClosedConsumerGroup) {
					return
				}
				log.Panicf("Error from consumer: %v", err)
			}
			// check if context was cancelled, signaling that the consumer should stop
			if ctx.Err() != nil {
				return
			}
			consumer.ready = make(chan bool)
		}
	}()

	<-consumer.ready // Await till the consumer has been set up
	log.Println("Sarama consumer up and running!...")

	sigusr1 := make(chan os.Signal, 1)

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGINT, syscall.SIGTERM)

	for keepRunning {
		select {
		case <-ctx.Done():
			log.Println("terminating: context cancelled")
			keepRunning = false
		case <-sigterm:
			log.Println("terminating: via signal")
			keepRunning = false
		case <-sigusr1:
			toggleConsumptionFlow(client, &consumptionIsPaused)
		}
	}
	cancel()
	wg.Wait()
	if err = client.Close(); err != nil {
		log.Panicf("Error closing client: %v", err)
	}
}

// 虚假的SocketIO：
// 这个Socket.io的实现逻辑显然和普通的不太一样
// 它不区分真正的订阅模式，只区分本地的事件驱动机制
// 当收到请求的消息是某个事件的时候，就触发本地的事件
// 它实际上并没有让对方订阅，继而它的Emit和普通的也不太一样，Emit代表回信，EmitTo代表给某个用户发送消息
// 基于这个思路来讲，实现从别人那里做转发桥的机制，应该是如下情况：
// 1. Ambari Stomp客户端（或者其他客户端）订阅，它是单个节点的
// 2. 用户连接，并通过权限判断它是否应该接收对应的订阅（有没有看那个页面的权限）
// 3. Stomp收到消息后，给所有有权限的用户推送消息
// 4. 前端需要根据消息动态修改展示。
// 5. 有可能需要一个数据库来保证操作对应的数据能正确填充
// 6. 只是实现Ambari的话，引入Socket.io不必要，使用Melody应该足以满足需求，但可以借鉴对应的消息回送机制，以及Onebot11的机制如事件机制和回声机制
// 回声机制用来控制返回的数据是否是本次运行产生的？
// Ambari采用了订阅的机制来实现，似乎是因为它是管理员，通过订阅的不同，切换了对应要获取的数据
// 现在的前端应该不需要，因为前端清楚知道用户是谁，后端清楚知道它有什么订阅消息。说白了就是后端全干:(
func main() {
	// The key for the map is message.to
	clients := make(map[string]string)

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		// IsWebSocketUpgrade checks if the client requested an upgrade to the WebSocket protocol.
		if socketws.IsWebSocketUpgrade(c) {
			c.Set("allowed", true)
			c.Next()
		} else {
			// Return an error if it's not a WebSocket upgrade
			c.JSON(http.StatusUpgradeRequired, gin.H{"error": "Upgrade required"})
			c.Abort()
		}
	})
	instance = socketio.NewSocketInstance()
	// Multiple event handling supported
	instance.On(socketio.EventConnect, func(ep *socketio.EventPayload) {
		fmt.Printf("Connection event 1 - User: %s", ep.Kws.GetStringAttribute("user_id"))
	})

	// 针对不同的事件进行逻辑处理
	instance.On("CUSTOM_EVENT", func(ep *socketio.EventPayload) {
		fmt.Printf("Custom event - User: %s", ep.Kws.GetStringAttribute("user_id"))
		// --->

		// DO YOUR BUSINESS HERE

		// --->
	})

	// On message event
	instance.On(socketio.EventMessage, func(ep *socketio.EventPayload) {

		fmt.Printf("Message event - User: %s - Message: %s", ep.Kws.GetStringAttribute("user_id"), string(ep.Data))

		message := MessageObject{}

		// Unmarshal the json message
		// {
		//  "from": "<user-id>",
		//  "to": "<recipient-user-id>",
		//  "event": "CUSTOM_EVENT",
		//  "data": "hello"
		//}
		err := json.Unmarshal(ep.Data, &message)
		if err != nil {
			fmt.Println(err)
			return
		}

		// Event逻辑分发
		if message.Event != "" {
			// 将整个数据塞过去
			ep.Kws.Fire(message.Event, []byte(message.Data))
		}
		// 发送给指定用户
		err = ep.Kws.EmitTo(clients[message.To], ep.Data, socketio.TextMessage)
		if err != nil {
			fmt.Println(err)
		}
	})
	// On disconnect event
	instance.On(socketio.EventDisconnect, func(ep *socketio.EventPayload) {
		// Remove the user from the local clients
		delete(clients, ep.Kws.GetStringAttribute("user_id"))
		fmt.Printf("Disconnection event - User: %s", ep.Kws.GetStringAttribute("user_id"))
	})

	// On close event
	// This event is called when the server disconnects the user actively with .Close() method
	instance.On(socketio.EventClose, func(ep *socketio.EventPayload) {
		// Remove the user from the local clients
		delete(clients, ep.Kws.GetStringAttribute("user_id"))
		fmt.Printf("Close event - User: %s", ep.Kws.GetStringAttribute("user_id"))
	})

	// On error event
	instance.On(socketio.EventError, func(ep *socketio.EventPayload) {
		fmt.Printf("Error event - User: %s", ep.Kws.GetStringAttribute("user_id"))
	})

	r.GET("/ws/:id", instance.New(func(kws *socketio.Websocket) {

		// Retrieve the user id from endpoint
		userId := kws.Params("id")

		// Add the connection to the list of the connected clients
		// The UUID is generated randomly and is the key that allow
		// socketio to manage Emit/EmitTo/Broadcast
		clients[userId] = kws.UUID

		// Every websocket connection has an optional session key => value storage
		kws.SetAttribute("user_id", userId)

		//Broadcast to all the connected users the newcomer
		kws.Broadcast([]byte(fmt.Sprintf("New user connected: %s and UUID: %s", userId, kws.UUID)), true, socketio.TextMessage)
		//Write welcome message
		kws.Emit([]byte(fmt.Sprintf("Hello user: %s with UUID: %s", userId, kws.UUID)), socketio.TextMessage)
	}))
	// 这种写法不太优雅。
	go ReceiveKafkaData()
	err := r.Run("0.0.0.0:13000")
	if err != nil {
		fmt.Println("错误")
		return
	}

	// TODO:
	// 目前来看，socket.io这个似乎是单个的，做不了多个WebSocket实例的情况
	// 不过理论上，通过不同的用户ID和Path，应当能做到伪多实例，这似乎也足够了，貌似也符合设想？

}
