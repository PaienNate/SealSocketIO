package main

import (
	"encoding/json"
	"fmt"
	"github.com/PaienNate/SealSocketIO/socketio"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{} // use default options

var clients = make(map[string]string)

// MessageObject Basic chat message object
type MessageObject struct {
	Data  string `json:"data"`
	From  string `json:"from"`
	Event string `json:"event"`
	To    string `json:"to"`
}

func main() {
	// 创建WS实例
	instance := socketio.NewSocketInstance()
	instance.On("EVENT_TEST", func(ep *socketio.EventPayload) {
		fmt.Println(ep.Data)
	})

	instance.On(socketio.EventMessage, func(ep *socketio.EventPayload) {
		// 对于Onebot协议，是谁发来的根本不重要
		fmt.Printf("Message event - Message: %s", string(ep.Data))
		// 在这里我们到时候用gjson解析数据
		message := MessageObject{}
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
	})

	instance.On(socketio.EventConnect, func(ep *socketio.EventPayload) {
		fmt.Println("连接成功")
	})

	// 初始连接
	conn, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:8080/ws", nil)
	if err != nil {
		fmt.Println("初始连接失败:", err)
		return
	}

	instance.On(socketio.EventDisconnect, func(ep *socketio.EventPayload) {
		fmt.Println("断开连接，尝试重连")
		conn, _, err = websocket.DefaultDialer.Dial("ws://127.0.0.1:8080/ws", nil)
		if err != nil {
			fmt.Println("初始连接失败:", err)
		}
		instance.NewClient(func(kws *socketio.WebsocketWrapper) {
			// 此时的BroadCast无效，因为Broadcast只会发给除了自己以外的客户端，不可能存在这样的客户端
			kws.Broadcast([]byte(fmt.Sprintf("New user connected:  and UUID: %s", kws.UUID)), true, socketio.TextMessage)
			// Emit发送消息有效，获取的KWS 的UUID就是刚刚连接的ws的UUID
			kws.Emit([]byte(fmt.Sprintf("Hello user:  with UUID: %s", kws.UUID)), socketio.TextMessage)
		}, conn)
	})

	instance.NewClient(func(kws *socketio.WebsocketWrapper) {
		// 此时的BroadCast无效，因为Broadcast只会发给除了自己以外的客户端，不可能存在这样的客户端
		kws.Broadcast([]byte(fmt.Sprintf("New user connected:  and UUID: %s", kws.UUID)), true, socketio.TextMessage)
		// Emit发送消息有效，获取的KWS 的UUID就是刚刚连接的ws的UUID
		kws.Emit([]byte(fmt.Sprintf("Hello user:  with UUID: %s", kws.UUID)), socketio.TextMessage)
	}, conn)

	// 塞个循环别结束掉
	select {}
}
