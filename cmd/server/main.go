package main

import (
	"net/http"

	"github.com/devnull-twitch/scrbble-backend/pkg/scrbble"
	"github.com/gorilla/websocket"
)

func main() {
	var upgrader = &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	roomManager := scrbble.CreateManager()
	go roomManager.Start()

	scrbbleHandlers := scrbble.CreateHttpHandlers(upgrader, roomManager)
	http.HandleFunc("/make_room", scrbbleHandlers.AddRoom())
	http.HandleFunc("/ws", scrbbleHandlers.Connect())

	http.ListenAndServe(":8090", nil)
}
