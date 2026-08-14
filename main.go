package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type WellState string

const (
	Producing WellState = "PRODUCING"
	ShutIn    WellState = "SHUT_IN"
	Fault     WellState = "FAULT"
)

type Well struct {
	ID          string
	State       WellState
	Pressure    float64
	Temperature float64
	FlowRate    float64
	ValveOpen   bool
	PumpRunning bool
}

type Telemetry struct {
	Timestamp   time.Time `json:"timestamp"`
	WellID      string    `json:"well_id"`
	State       WellState `json:"state"`
	Pressure    float64   `json:"pressure_psi"`
	Temperature float64   `json:"temperature_c"`
	FlowRate    float64   `json:"flow_rate_bpd"`
	ValveOpen   bool      `json:"valve_open"`
	PumpRunning bool      `json:"pump_running"`
}

func (w *Well) Update() {
	switch w.State {
	case Producing:
		w.updateProducing()

	case ShutIn:
		w.updateShutIn()

	case Fault:
		w.updateFault()
	}
}

func (w *Well) updateProducing() {
	// Natural fluctuations in the well.
	pressureChange := rand.Float64()*20 - 10
	temperatureChange := rand.Float64()*1 - 0.5

	w.Pressure += pressureChange
	w.Temperature += temperatureChange

	if w.ValveOpen && w.PumpRunning {
		// Flow changes gradually.
		w.FlowRate += rand.Float64()*10 - 5

		// Production causes some pressure decline.
		w.Pressure -= 5
	} else {
		// If production equipment isn't operating,
		// flow starts decreasing.
		w.FlowRate -= 20
	}

	// Keep flow within reasonable limits.
	if w.FlowRate < 0 {
		w.FlowRate = 0
	}

	if w.FlowRate > 5000 {
		w.FlowRate = 5000
	}

	// Keep pressure within simulator limits.
	if w.Pressure < 2500 {
		w.Pressure = 2500
	}

	if w.Pressure > 4000 {
		w.Pressure = 4000
	}

	// Keep temperature within simulator limits.
	if w.Temperature < 70 {
		w.Temperature = 70
	}

	if w.Temperature > 100 {
		w.Temperature = 100
	}
}

func (w *Well) updateShutIn() {
	// Production stops.
	w.FlowRate -= 100

	if w.FlowRate < 0 {
		w.FlowRate = 0
	}

	// Pressure gradually builds when the well is shut in.
	w.Pressure += 15

	if w.Pressure > 4000 {
		w.Pressure = 4000
	}

	// Temperature slowly changes.
	w.Temperature += rand.Float64()*0.4 - 0.2
}

func (w *Well) updateFault() {
	// A fault causes production to deteriorate.
	w.FlowRate -= 200

	if w.FlowRate < 0 {
		w.FlowRate = 0
	}

	// Pressure and temperature increase.
	w.Pressure += 20
	w.Temperature += 1

	if w.Pressure > 4500 {
		w.Pressure = 4500
	}

	if w.Temperature > 120 {
		w.Temperature = 120
	}
}

func (w *Well) GetTelemetry() Telemetry {
	return Telemetry{
		Timestamp:   time.Now(),
		WellID:      w.ID,
		State:       w.State,
		Pressure:    w.Pressure,
		Temperature: w.Temperature,
		FlowRate:    w.FlowRate,
		ValveOpen:   w.ValveOpen,
		PumpRunning: w.PumpRunning,
	}
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer conn.Close()

	for {
		well := Well{
			ID:          "WELL-001",
			State:       Producing,
			Pressure:    3200,
			Temperature: 82,
			FlowRate:    2500,
			ValveOpen:   true,
			PumpRunning: true,
		}
		well.Update()
		telemetry := well.GetTelemetry()

		err := conn.WriteJSON(telemetry)
		if err != nil {
			break
		}

		time.Sleep(time.Second)
	}
}

func main() {
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/well/telementry", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Println("Request hits well telementry")
	})
	fmt.Println("server started on http://localhost:8080/")
	http.ListenAndServe(":8080", nil)

}
