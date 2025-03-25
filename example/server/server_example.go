package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"websocket/socketio"
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

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	// 获取path

	// defer c.Close()
	instance := socketio.NewSocketInstance()
	instance.On("EVENT_TEST", func(ep *socketio.EventPayload) {
		fmt.Println(ep.Data)
	})

	instance.On(socketio.EventMessage, func(ep *socketio.EventPayload) {

		fmt.Printf("Message event - User: %s - Message: %s", ep.Kws.GetStringAttribute("user_id"), string(ep.Data))

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
		// 发送给指定用户
		err = ep.Kws.EmitTo(clients[message.To], ep.Data, socketio.TextMessage)
		if err != nil {
			fmt.Println(err)
		}
	})

	handler := instance.New(func(kws *socketio.WebsocketWrapper) {
		// Broadcast to all the connected users the newcomer
		kws.Broadcast([]byte(fmt.Sprintf("New user connected:  and UUID: %s", kws.UUID)), true, socketio.TextMessage)
		// Write welcome message
		kws.Emit([]byte(fmt.Sprintf("Hello user:  with UUID: %s", kws.UUID)), socketio.TextMessage)
	}, c)
	handler(w, r)
}

func main() {
	// 创建WS实例
	// 注册 WebSocket 路由
	http.HandleFunc("/echo", echo)

	log.Fatal(http.ListenAndServe("0.0.0.0:3002", nil))
	// 创建WebSocket Conn
}
