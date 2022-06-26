package scrbble

import (
	"fmt"

	"github.com/solarlune/resolv"
)

type Room struct {
	Players        []*Player
	Pebbles        map[int]*resolv.Object
	PhysicsManager *PhysicsManager
	RoomID         string
	Active         bool
}

type RoomManager struct {
	addRoomChan     chan *Room
	removeRoom      chan string
	getRoomRequests chan GetRoomRequest
}

var nextRoomID int = 1

type GetRoomRequest struct {
	RoomID   string
	Response chan *Room
}

func CreateManager() *RoomManager {
	return &RoomManager{
		addRoomChan:     make(chan *Room),
		removeRoom:      make(chan string),
		getRoomRequests: make(chan GetRoomRequest),
	}
}

func (m *RoomManager) Start() {
	roomStorage := make(map[string]*Room)

	for {
		select {
		case newRoom := <-m.addRoomChan:
			roomStorage[newRoom.RoomID] = newRoom

		case roomID := <-m.removeRoom:
			delete(roomStorage, roomID)

		case req := <-m.getRoomRequests:
			room, ok := roomStorage[req.RoomID]
			if !ok {
				close(req.Response)
				continue
			}

			req.Response <- room
		}
	}
}

func (m *RoomManager) CreateRoom() *Room {
	phyM := CreatePhysicsManager()
	go phyM.Start()

	roomID := fmt.Sprintf("room-%d", nextRoomID)
	nextRoomID++

	room := &Room{
		Players:        []*Player{},
		Pebbles:        make(map[int]*resolv.Object),
		RoomID:         roomID,
		Active:         true,
		PhysicsManager: phyM,
	}
	m.addRoomChan <- room
	return room
}

func (m *RoomManager) SendGetRequest(r GetRoomRequest) {
	m.getRoomRequests <- r
}

func createBorderObject(x, y, w, h float64) *resolv.Object {
	obj := resolv.NewObject(x, y, w, h)
	obj.SetShape(resolv.NewRectangle(0, 0, w, h))

	return obj
}
