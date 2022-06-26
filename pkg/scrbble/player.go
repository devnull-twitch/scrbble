package scrbble

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/solarlune/resolv"
)

type Player struct {
	Conn               *websocket.Conn
	PlayerTag          string
	PhysicsMessageChan chan PhysicsMessage
	Points             int
	Active             bool
	Head               *resolv.Object
	Parts              []*resolv.Object
	ServerMsgs         chan serverMessage
	Room               *Room
}

type serverMessage struct {
	Type     string  `json:"message_type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	ID       int     `json:"resource_id"`
	PlayerID string  `json:"player_id"`
}

type clientMessage struct {
	T float64 `json:"t"`
}

var nextPlayerID int = 1

func CreatePlayer(conn *websocket.Conn, room *Room) *Player {
	playerTag := fmt.Sprintf("player-%d", nextPlayerID)
	nextPlayerID++

	netOutMessages := make(chan serverMessage)
	newPlayer := &Player{
		Conn:       conn,
		PlayerTag:  playerTag,
		Points:     0,
		Active:     true,
		ServerMsgs: netOutMessages,
		Room:       room,
	}

	physicsMessages := make(chan PhysicsMessage)
	phym := room.PhysicsManager
	phym.addPlayer <- playerPhysicsCreateRequest{
		ID:         playerTag,
		ServerChan: physicsMessages,
		HeadX:      300,
		HeadY:      300,
		BodyParts: []bodyPartyCoords{
			{X: 300, Y: 300},
		},
	}

	conn.SetCloseHandler(newPlayer.CloseHandler)

	go func() {
		roomLog := logrus.WithField("room", room.RoomID).WithField("player_id", newPlayer.PlayerTag)
		for newPlayer.Active {
			select {
			case physMsg := <-physicsMessages:
				switch physMsg.Type {
				case PhyMsgAddPoint:
					newPlayer.Points++
					if newPlayer.Points%3 == 0 {
						conn.WriteJSON(serverMessage{
							Type:     "add_part",
							PlayerID: newPlayer.PlayerTag,
						})
						roomLog.Info("send add part")
						go func() {
							phym.addPart <- playerPhysicsAddPartRequest{ID: newPlayer.PlayerTag}
						}()
					}

				case PhyMsgRemovePebble:
					conn.WriteJSON(serverMessage{
						Type: "pebble-remove",
						ID:   physMsg.ID,
					})
					logrus.Info("send pebble deletion")

				case PhyMsgAddPebble:
					conn.WriteJSON(serverMessage{
						Type: "pebble",
						ID:   physMsg.ID,
						X:    physMsg.X,
						Y:    physMsg.Y,
					})
					logrus.Debug("send new pebble")

				case PhyMsgGameOver:
					newPlayer.Active = false
					conn.WriteJSON(serverMessage{
						Type: "game_over",
					})
					logrus.Info("send game over")

				case PhyMsgPos:
					conn.WriteJSON(serverMessage{
						Type:     "pos",
						X:        physMsg.X,
						Y:        physMsg.Y,
						PlayerID: physMsg.PlayerID,
					})
					logrus.Debug("send game over")
				}

			case netMsg := <-netOutMessages:
				conn.WriteJSON(netMsg)
			}
		}
	}()

	clientMsg := &clientMessage{}
	errCount := 0
	go func() {
		for {
			if err := conn.ReadJSON(clientMsg); err != nil {
				errCount++
				if errCount > 10 {
					conn.Close()
					return
				}
			}

			phym.updateRotation <- UpdatePlayerRotation{
				ID: newPlayer.PlayerTag,
				T:  clientMsg.T,
			}
		}
	}()

	newPlayer.ServerMsgs <- serverMessage{
		Type:     "youare",
		PlayerID: newPlayer.PlayerTag,
	}

	return newPlayer
}

func (p *Player) CloseHandler(code int, text string) error {
	select {
	case p.Room.PhysicsManager.removePlayer <- p.PlayerTag:
	case <-time.After(time.Second):
		logrus.Fatal("Player removal from pysics module timed out")
	}

	p.Active = false

	if p.Room == nil {
		logrus.Fatal("player closed without room")
		return fmt.Errorf("player closed without room")
	}

	filteredPlayers := make([]*Player, 0, len(p.Room.Players))
	for _, other := range p.Room.Players {
		if other.Conn != p.Conn {
			filteredPlayers = append(filteredPlayers, p)
			other.ServerMsgs <- serverMessage{
				Type:     "disconnect",
				PlayerID: p.PlayerTag,
			}
		} else {
			p.Room.PhysicsManager.removePlayer <- p.PlayerTag
		}
	}

	p.Room.Players = filteredPlayers
	logrus.WithField("player_id", p.PlayerTag).Info("removed from room")
	return nil
}
