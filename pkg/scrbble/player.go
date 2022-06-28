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
	ID                 string
	PhysicsMessageChan chan PhysicsMessage
	Points             int
	Active             bool
	Head               *resolv.Object
	Parts              []*resolv.Object
	ServerMsgs         chan serverMessage
	Room               *Room
	Color              string
}

type serverMessage struct {
	Type     string            `json:"message_type"`
	X        float64           `json:"x"`
	Y        float64           `json:"y"`
	T        float64           `json:"t"`
	ID       int               `json:"resource_id"`
	PlayerID string            `json:"player_id"`
	Payload  map[string]string `json:"payload"`
}

type clientMessage struct {
	T float64 `json:"t"`
}

var nextPlayerID int = 1

func CreatePlayer(
	conn *websocket.Conn,
	room *Room,
	color string,
) *Player {
	id := fmt.Sprintf("player-%d", nextPlayerID)
	nextPlayerID++

	netOutMessages := make(chan serverMessage)
	newPlayer := &Player{
		Conn:       conn,
		ID:         id,
		Points:     0,
		Active:     true,
		ServerMsgs: netOutMessages,
		Room:       room,
		Color:      color,
	}

	conn.SetCloseHandler(newPlayer.CloseHandler)

	physicsMessages := make(chan PhysicsMessage)
	phym := room.PhysicsManager
	phym.addPlayer <- playerPhysicsCreateRequest{
		ID:         id,
		ServerChan: physicsMessages,
		HeadX:      300,
		HeadY:      300,
		BodyParts: []bodyPartyCoords{
			{X: 300, Y: 300},
		},
	}

	go func() {
		roomLog := logrus.WithField("room", room.RoomID).WithField("player_id", newPlayer.ID)
		for newPlayer.Active {
			select {
			case physMsg := <-physicsMessages:
				switch physMsg.Type {
				case PhyMsgAddPoint:
					newPlayer.Points++
					if newPlayer.Points%3 == 0 {
						roomLog.Info("send add part")
						go func() {
							for playerID, otherPlayer := range room.Players {
								select {
								case otherPlayer.ServerMsgs <- serverMessage{
									Type:     "add_part",
									PlayerID: newPlayer.ID,
								}:
								case <-time.After(time.Second):
									logrus.WithField("player_id", playerID).Warn("send of part update timeout")
								}
							}
						}()

						go func() {
							phym.addPart <- playerPhysicsAddPartRequest{ID: newPlayer.ID}
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
					go func() {
						select {
						case phym.removePlayer <- newPlayer.ID:
						case <-time.After(time.Second):
							logrus.Warn("Player removal from pysics module timed out")
						}
					}()
					logrus.Info("send game over")

				case PhyMsgPos:
					conn.WriteJSON(serverMessage{
						Type:     "pos",
						X:        physMsg.X,
						Y:        physMsg.Y,
						PlayerID: physMsg.PlayerID,
						T:        physMsg.T,
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
		for newPlayer.Active {
			if err := conn.ReadJSON(clientMsg); err != nil {
				errCount++
				if errCount > 10 {
					conn.Close()
					return
				}
			}

			phym.updateRotation <- UpdatePlayerRotation{
				ID: newPlayer.ID,
				T:  clientMsg.T,
			}
		}
	}()

	newPlayer.ServerMsgs <- serverMessage{
		Type:     "youare",
		PlayerID: newPlayer.ID,
	}

	for _, otherPlayer := range room.Players {
		// notify all other players about new player
		otherPlayer.ServerMsgs <- serverMessage{
			Type:     "new_player",
			PlayerID: newPlayer.ID,
			Payload: map[string]string{
				"color": newPlayer.Color,
			},
		}

		// sync existing player to new player
		newPlayer.ServerMsgs <- serverMessage{
			Type:     "new_player",
			PlayerID: otherPlayer.ID,
			Payload: map[string]string{
				"color": otherPlayer.Color,
			},
		}
	}

	return newPlayer
}

func (p *Player) CloseHandler(code int, text string) error {
	if p.Room == nil {
		logrus.Fatal("player closed without room")
		return fmt.Errorf("player closed without room")
	}

	go func() {
		select {
		case p.Room.PhysicsManager.removePlayer <- p.ID:
		case <-time.After(time.Second):
			logrus.Warn("Player removal from pysics module timed out")
		}
	}()

	p.Active = false

	go func() {
		p.Room.RoomManager.SendRemovePlayerRequest(p.Room.RoomID, p.ID)
	}()

	logrus.WithField("player_id", p.ID).Info("removed from room")
	return nil
}
