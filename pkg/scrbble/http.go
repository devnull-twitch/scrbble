package scrbble

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type HttpHandlers struct {
	upgrader    *websocket.Upgrader
	roomManager *RoomManager
}

func CreateHttpHandlers(
	upgrader *websocket.Upgrader,
	roomManager *RoomManager,
) *HttpHandlers {
	return &HttpHandlers{
		upgrader:    upgrader,
		roomManager: roomManager,
	}
}

type newRoomResponse struct {
	NewRoomID string `json:"room_id"`
}

func (h *HttpHandlers) AddRoom() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		if r.Method == "OPTIONS" {
			w.Header().Add("Access-Control-Allow-Methods", "POST,OPTIONS")
			w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		room := h.roomManager.CreateRoom()

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		resp := &newRoomResponse{NewRoomID: room.RoomID}
		respBytes, err := json.Marshal(resp)
		if err != nil {
			panic(err)
		}
		w.Write(respBytes)
	}
}

func (h *HttpHandlers) Connect() http.HandlerFunc {
	colorPattern, err := regexp.Compile(`^?([a-f\d]{6})$`)
	if err != nil {
		panic(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		queryValue := r.URL.Query()
		if !queryValue.Has("rk") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		color := queryValue.Get("color")
		if color == "" {
			color = "FFFFFF"
		}

		if !colorPattern.MatchString(color) {
			logrus.WithField("color", color).Error("invalid color code given")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		respChan := make(chan *Room)
		h.roomManager.SendGetRequest(queryValue.Get("rk"), respChan)
		var room *Room
		select {
		case room = <-respChan:
		case <-time.After(time.Second):
		}

		if room == nil {
			logrus.WithField("room_id", queryValue.Get("rk")).Error("room not found")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}

		newPlayer := CreatePlayer(conn, room, color)

		h.roomManager.SendAddPlayerRequest(room.RoomID, newPlayer)

		syncRespChan := make(chan PebbleSyncResponse)
		room.PhysicsManager.sync <- PebbleSyncRequest{ResponseChan: syncRespChan}
		syncResp := <-syncRespChan
		for _, pebbles := range syncResp.Pebbles {
			newPlayer.ServerMsgs <- serverMessage{
				Type: "pebble",
				X:    pebbles.X,
				Y:    pebbles.Y,
				ID:   pebbles.ID,
			}
		}
	}
}
