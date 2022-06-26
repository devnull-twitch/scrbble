package scrbble

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/solarlune/resolv"
)

type Room struct {
	Players        []*Player
	Pebbles        map[int]*resolv.Object
	PhysicsManager *PhysicsManager
	RoomManager    *RoomManager
	RoomID         string
	Active         bool
}

type addPlayerRequest struct {
	RoomID string
	Player *Player
}

type removePlayerRequest struct {
	RoomID   string
	PlayerID string
}

type RoomManager struct {
	addRoomChan          chan *Room
	removeRoom           chan string
	getRoomRequests      chan getRoomRequest
	addPlayerRequests    chan addPlayerRequest
	removePlayerRequests chan removePlayerRequest
}

var nextRoomID int = 1

type getRoomRequest struct {
	RoomID   string
	Response chan *Room
}

func CreateManager() *RoomManager {
	return &RoomManager{
		addRoomChan:          make(chan *Room),
		removeRoom:           make(chan string),
		getRoomRequests:      make(chan getRoomRequest),
		addPlayerRequests:    make(chan addPlayerRequest),
		removePlayerRequests: make(chan removePlayerRequest),
	}
}

func (m *RoomManager) Start() {
	roomStorage := make(map[string]*Room)

	cleanupTicket := time.NewTicker(time.Second * 2)
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

		case req := <-m.addPlayerRequests:
			room, ok := roomStorage[req.RoomID]
			if !ok {
				continue
			}

			room.Players = append(room.Players, req.Player)

		case req := <-m.removePlayerRequests:
			room, ok := roomStorage[req.RoomID]
			if !ok {
				continue
			}

			filteredList := make([]*Player, 0, len(room.Players))
			for _, otherPlayer := range room.Players {
				if otherPlayer.ID != req.PlayerID {
					filteredList = append(filteredList, otherPlayer)
					otherPlayer.ServerMsgs <- serverMessage{
						Type:     "disconnect",
						PlayerID: req.PlayerID,
					}
				}
			}
			room.Players = filteredList

		case <-cleanupTicket.C:
			for roomID, r := range roomStorage {
				if len(r.Players) <= 0 {
					r.PhysicsManager.deleteSelf <- true
					delete(roomStorage, roomID)
					logrus.WithField("room_id", roomID).Info("Delete room")
				}
			}
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
		RoomManager:    m,
	}
	m.addRoomChan <- room
	return room
}

func (m *RoomManager) SendGetRequest(roomID string, responseChan chan *Room) {
	m.getRoomRequests <- getRoomRequest{
		RoomID:   roomID,
		Response: responseChan,
	}
}

func (m *RoomManager) SendAddPlayerRequest(roomID string, player *Player) {
	m.addPlayerRequests <- addPlayerRequest{
		RoomID: roomID,
		Player: player,
	}
}

func (m *RoomManager) SendRemovePlayerRequest(roomID, playerID string) {
	m.removePlayerRequests <- removePlayerRequest{
		RoomID:   roomID,
		PlayerID: playerID,
	}
}

func createBorderObject(x, y, w, h float64) *resolv.Object {
	obj := resolv.NewObject(x, y, w, h)
	obj.SetShape(resolv.NewRectangle(0, 0, w, h))

	return obj
}
