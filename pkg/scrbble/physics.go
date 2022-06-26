package scrbble

import (
	"math"
	"math/rand"
	"time"

	"github.com/kvartborg/vector"
	"github.com/sirupsen/logrus"
	"github.com/solarlune/resolv"
)

type bodyPartyCoords struct {
	X float64
	Y float64
}

const (
	PhyMsgRemovePebble = "pebble_remove"
	PhyMsgAddPebble    = "pebble_add"
	PhyMsgAddPoint     = "add_point"
	PhyMsgGameOver     = "game_over"
	PhyMsgPos          = "pos"
)

type PhysicsMessage struct {
	Type     string
	ID       int
	X        float64
	Y        float64
	PlayerID string
}

type playerPhysicsCreateRequest struct {
	ID         string
	ServerChan chan PhysicsMessage
	HeadX      float64
	HeadY      float64
	BodyParts  []bodyPartyCoords
}

type playerPhysicsAddPartRequest struct {
	ID string
}

type PebbleSyncPosition struct {
	ID int
	X  float64
	Y  float64
}

type PebbleSyncResponse struct {
	Pebbles []PebbleSyncPosition
}

type PebbleSyncRequest struct {
	ResponseChan chan PebbleSyncResponse
}

type physicsPlayer struct {
	id          string
	t           float64
	head        *resolv.Object
	bps         []*resolv.Object
	outMessages chan PhysicsMessage
	active      bool
}

type UpdatePlayerRotation struct {
	ID string
	T  float64
}

type snakePartData struct {
	Buffer []vector.Vector
	Index  int
}

type PhysicsManager struct {
	space   *resolv.Space
	players map[string]*physicsPlayer

	deleteSelf     chan interface{}
	addPlayer      chan playerPhysicsCreateRequest
	addPart        chan playerPhysicsAddPartRequest
	sync           chan PebbleSyncRequest
	updateRotation chan UpdatePlayerRotation
	removePlayer   chan string
	nextPebbleID   int
	pebbles        map[int]*resolv.Object
}

func CreatePhysicsManager() *PhysicsManager {
	space := resolv.NewSpace(5000, 5000, 128, 128)
	space.Add(
		createBorderObject(0, 0, 1, 5000),    // right
		createBorderObject(0, 4999, 5000, 1), // bottom
		createBorderObject(4999, 0, 1, 5000), // left
		createBorderObject(0, 0, 5000, 1),    // top
	)

	return &PhysicsManager{
		space:          space,
		players:        map[string]*physicsPlayer{},
		deleteSelf:     make(chan interface{}),
		addPlayer:      make(chan playerPhysicsCreateRequest),
		removePlayer:   make(chan string),
		addPart:        make(chan playerPhysicsAddPartRequest),
		updateRotation: make(chan UpdatePlayerRotation),
		sync:           make(chan PebbleSyncRequest),
		pebbles:        make(map[int]*resolv.Object),
		nextPebbleID:   1,
	}
}

func (phym *PhysicsManager) Start() {
	defer func() {
		logrus.Info("physics loop ended")
	}()

	ticker := time.NewTicker(time.Second / 30)
	pebbleCreateTicker := time.NewTicker(time.Millisecond * 250)
	for {
		select {
		case <-phym.deleteSelf:
			return

		case req := <-phym.addPlayer:
			headObj := resolv.NewObject(req.HeadX, req.HeadY, 128, 128, req.ID)
			headObj.SetShape(resolv.NewRectangle(0, 0, 128, 128))
			phym.space.Add(headObj)

			bps := make([]*resolv.Object, 0, len(req.BodyParts))
			for _, bp := range req.BodyParts {
				bodyObj := resolv.NewObject(bp.X, bp.Y, 128, 128, req.ID)
				bodyObj.SetShape(resolv.NewRectangle(0, 0, 128, 128))
				bodyObj.Data = &snakePartData{
					Buffer: make([]vector.Vector, 15),
					Index:  0,
				}
				phym.space.Add(bodyObj)
				bps = append(bps, bodyObj)
			}

			phym.players[req.ID] = &physicsPlayer{
				id:          req.ID,
				outMessages: req.ServerChan,
				t:           0,
				head:        headObj,
				active:      true,
				bps:         bps,
			}

		case addPartReq := <-phym.addPart:
			p, ok := phym.players[addPartReq.ID]
			if !ok {
				logrus.WithField("player_id", addPartReq.ID).Error("player missing in physics module")
				continue
			}

			bodyObj := resolv.NewObject(p.head.X, p.head.Y, 128, 128, p.id)
			bodyObj.SetShape(resolv.NewRectangle(0, 0, 128, 128))
			bodyObj.Data = &snakePartData{
				Buffer: make([]vector.Vector, 15),
				Index:  0,
			}
			phym.space.Add(bodyObj)
			p.bps = append(p.bps, bodyObj)

		case removeId := <-phym.removePlayer:
			p, ok := phym.players[removeId]
			if !ok {
				continue
			}

			phym.space.Remove(p.head)
			for _, bp := range p.bps {
				phym.space.Remove(bp)
			}
			delete(phym.players, removeId)

		case syncReq := <-phym.sync:
			respPebbles := make([]PebbleSyncPosition, 0, len(phym.pebbles))
			for pebbleID, pebbleObj := range phym.pebbles {
				respPebbles = append(respPebbles, PebbleSyncPosition{X: pebbleObj.X, Y: pebbleObj.Y, ID: pebbleID})
			}
			syncReq.ResponseChan <- PebbleSyncResponse{Pebbles: respPebbles}

		case rotationReq := <-phym.updateRotation:
			p, ok := phym.players[rotationReq.ID]
			if !ok {
				continue
			}
			p.t = rotationReq.T

		case <-ticker.C:
			phym.tick()

		case <-pebbleCreateTicker.C:
			pebbleObj := resolv.NewObject(float64(rand.Intn(5000)), float64(rand.Intn(5000)), 64, 64, "pebble")
			pebbleObj.SetShape(resolv.NewRectangle(0, 0, 64, 64))
			pebbleObj.Data = phym.nextPebbleID
			phym.pebbles[phym.nextPebbleID] = pebbleObj
			phym.space.Add(pebbleObj)

			for _, player := range phym.players {
				player.outMessages <- PhysicsMessage{
					Type: PhyMsgAddPebble,
					X:    pebbleObj.X,
					Y:    pebbleObj.Y,
					ID:   phym.nextPebbleID,
				}
			}
			phym.nextPebbleID++

		}
	}
}

func (phym *PhysicsManager) tick() {
	for _, p := range phym.players {
		mx := 3 * math.Cos(p.t)
		my := 3 * math.Sin(p.t)

		coll := p.head.Check(mx, my)
		if coll != nil {
			if !phym.checkIntersections(coll, p, mx, my) {
				continue
			}
		}

		cx := p.head.X
		cy := p.head.Y
		currentVector := vector.Vector{cx, cy}
		for _, part := range p.bps {
			if len(currentVector) > 1 {
				currentVector = pushPosition(part, currentVector)
			}
			part.Update()
		}

		p.head.X = p.head.X + mx
		p.head.Y = p.head.Y + my
		p.head.Update()

		for _, otherPlayer := range phym.players {
			otherPlayer.outMessages <- PhysicsMessage{
				Type:     PhyMsgPos,
				X:        p.head.X,
				Y:        p.head.Y,
				PlayerID: p.id,
			}
		}
	}
}

func (phym *PhysicsManager) checkIntersections(
	coll *resolv.Collision,
	p *physicsPlayer,
	mx float64,
	my float64,
) bool {
	for _, colObj := range coll.Objects {
		// ignore all collisions with self
		if colObj.HasTags(p.id) {
			continue
		}

		if p.head.Shape.Intersection(mx, my, colObj.Shape) != nil {
			// only pebbles have data == int
			if id, isInt := colObj.Data.(int); isInt {
				for _, otherPlayer := range phym.players {
					otherPlayer.outMessages <- PhysicsMessage{
						Type: PhyMsgRemovePebble,
						ID:   id,
					}
				}

				phym.space.Remove(colObj)

				p.outMessages <- PhysicsMessage{
					Type: PhyMsgAddPoint,
				}
			} else {
				p.outMessages <- PhysicsMessage{
					Type: PhyMsgGameOver,
				}
				p.active = false
				return false
			}
		}
	}

	return true
}

func pushPosition(snakePart *resolv.Object, pos vector.Vector) vector.Vector {
	data := snakePart.Data.(*snakePartData)

	var returnValue vector.Vector = nil
	if lastPos := data.Buffer[data.Index]; len(lastPos) > 1 {
		returnValue = lastPos
		snakePart.X = lastPos.X()
		snakePart.Y = lastPos.Y()
	}

	data.Buffer[data.Index] = pos
	data.Index++

	if data.Index > len(data.Buffer)-1 {
		data.Index = 0
	}

	return returnValue
}
