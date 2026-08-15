package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

// ── Types ──────────────────────────────────────────────────────────────────

type WellState string
type AlertLevel string

const (
	Producing WellState = "PRODUCING"
	ShutIn    WellState = "SHUT_IN"
	Fault     WellState = "FAULT"
)

const (
	AlertNone     AlertLevel = "NONE"
	AlertWarning  AlertLevel = "WARNING"
	AlertCritical AlertLevel = "CRITICAL"
)

// ── Alert thresholds ───────────────────────────────────────────────────────

const (
	PressureWarnHigh = 3700.0
	PressureCritHigh = 4100.0
	TempWarnHigh     = 88.0
	TempCritHigh     = 98.0
	FlowWarnLow      = 800.0
	FlowCritLow      = 200.0
)

// ── Structs ────────────────────────────────────────────────────────────────

type Alert struct {
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	Level     AlertLevel `json:"level"`
	Value     float64    `json:"value"`
	Threshold float64    `json:"threshold"`
}

type Well struct {
	ID          string
	State       WellState
	Pressure    float64
	Temperature float64
	FlowRate    float64
	ValveOpen   bool
	PumpRunning bool
	stateTicks  int // ticks spent in current state
}

type Telemetry struct {
	Timestamp   time.Time  `json:"timestamp"`
	WellID      string     `json:"well_id"`
	State       WellState  `json:"state"`
	Pressure    float64    `json:"pressure_psi"`
	Temperature float64    `json:"temperature_c"`
	FlowRate    float64    `json:"flow_rate_bpd"`
	ValveOpen   bool       `json:"valve_open"`
	PumpRunning bool       `json:"pump_running"`
	Alerts      []Alert    `json:"alerts"`
	AlertLevel  AlertLevel `json:"alert_level"`
}

// ── Alert detection ────────────────────────────────────────────────────────

func (w *Well) CheckAlerts() ([]Alert, AlertLevel) {
	var alerts []Alert
	maxLevel := AlertNone

	bump := func(l AlertLevel) {
		if l == AlertCritical || (l == AlertWarning && maxLevel == AlertNone) {
			maxLevel = l
		}
	}

	// Pressure
	if w.Pressure >= PressureCritHigh {
		alerts = append(alerts, Alert{
			Code:      "OVERPRESSURE_CRIT",
			Message:   fmt.Sprintf("Critical overpressure: %.0f PSI exceeds %.0f PSI limit", w.Pressure, PressureCritHigh),
			Level:     AlertCritical,
			Value:     w.Pressure,
			Threshold: PressureCritHigh,
		})
		bump(AlertCritical)
	} else if w.Pressure >= PressureWarnHigh {
		alerts = append(alerts, Alert{
			Code:      "HIGH_PRESSURE",
			Message:   fmt.Sprintf("High pressure: %.0f PSI approaching safety limit", w.Pressure),
			Level:     AlertWarning,
			Value:     w.Pressure,
			Threshold: PressureWarnHigh,
		})
		bump(AlertWarning)
	}

	// Temperature
	if w.Temperature >= TempCritHigh {
		alerts = append(alerts, Alert{
			Code:      "OVERTEMP_CRIT",
			Message:   fmt.Sprintf("Critical overtemperature: %.1f°C exceeds %.0f°C limit", w.Temperature, TempCritHigh),
			Level:     AlertCritical,
			Value:     w.Temperature,
			Threshold: TempCritHigh,
		})
		bump(AlertCritical)
	} else if w.Temperature >= TempWarnHigh {
		alerts = append(alerts, Alert{
			Code:      "HIGH_TEMP",
			Message:   fmt.Sprintf("High temperature: %.1f°C approaching limit", w.Temperature),
			Level:     AlertWarning,
			Value:     w.Temperature,
			Threshold: TempWarnHigh,
		})
		bump(AlertWarning)
	}

	// Flow rate — only meaningful while producing
	if w.State == Producing {
		if w.FlowRate <= FlowCritLow {
			alerts = append(alerts, Alert{
				Code:      "LOW_FLOW_CRIT",
				Message:   fmt.Sprintf("Critical low flow: %.0f BPD — potential well loss", w.FlowRate),
				Level:     AlertCritical,
				Value:     w.FlowRate,
				Threshold: FlowCritLow,
			})
			bump(AlertCritical)
		} else if w.FlowRate <= FlowWarnLow {
			alerts = append(alerts, Alert{
				Code:      "LOW_FLOW",
				Message:   fmt.Sprintf("Low flow rate: %.0f BPD below minimum threshold", w.FlowRate),
				Level:     AlertWarning,
				Value:     w.FlowRate,
				Threshold: FlowWarnLow,
			})
			bump(AlertWarning)
		}
	}

	// Equipment mismatches
	if w.State == Producing && !w.ValveOpen {
		alerts = append(alerts, Alert{
			Code:      "VALVE_CLOSED_PRODUCING",
			Message:   "Surface valve closed while well is in PRODUCING state",
			Level:     AlertWarning,
			Value:     0,
			Threshold: 1,
		})
		bump(AlertWarning)
	}
	if w.State == Producing && !w.PumpRunning {
		alerts = append(alerts, Alert{
			Code:      "PUMP_STOPPED_PRODUCING",
			Message:   "ESP pump stopped while well is in PRODUCING state",
			Level:     AlertWarning,
			Value:     0,
			Threshold: 1,
		})
		bump(AlertWarning)
	}

	if alerts == nil {
		alerts = []Alert{}
	}
	return alerts, maxLevel
}

// ── State machine ──────────────────────────────────────────────────────────

func (w *Well) advanceState() {
	w.stateTicks++

	switch w.State {
	case Producing:
		// Critical conditions force a fault immediately.
		if w.Pressure >= PressureCritHigh || w.Temperature >= TempCritHigh {
			fmt.Printf("[WELL] Automatic FAULT trigger — P=%.0f T=%.1f\n", w.Pressure, w.Temperature)
			w.State = Fault
			w.PumpRunning = false
			w.stateTicks = 0
			return
		}
		// Random rare events (~0.5% / tick → roughly every 3 minutes).
		r := rand.Float64()
		if r < 0.005 {
			fmt.Println("[WELL] Random FAULT event triggered")
			w.State = Fault
			w.PumpRunning = false
			w.stateTicks = 0
		} else if r < 0.012 {
			fmt.Println("[WELL] Random SHUT_IN event triggered")
			w.State = ShutIn
			w.ValveOpen = false
			w.stateTicks = 0
		}

	case ShutIn:
		// Recover after 20–40 ticks.
		if w.stateTicks >= 20+rand.Intn(20) {
			fmt.Println("[WELL] Well recovering → PRODUCING")
			w.State = Producing
			w.ValveOpen = true
			w.PumpRunning = true
			w.stateTicks = 0
		}

	case Fault:
		// Emergency shutdown lasts 15–25 ticks then moves to SHUT_IN for stabilisation.
		if w.stateTicks >= 15+rand.Intn(10) {
			fmt.Println("[WELL] Fault cleared → SHUT_IN for stabilisation")
			w.State = ShutIn
			w.ValveOpen = false
			w.PumpRunning = false
			w.stateTicks = 0
		}
	}
}

// ── Physics updates ────────────────────────────────────────────────────────

func (w *Well) Update() {
	w.advanceState()

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
	w.Pressure += rand.Float64()*20 - 10
	w.Temperature += rand.Float64()*1 - 0.5

	if w.ValveOpen && w.PumpRunning {
		w.FlowRate += rand.Float64()*10 - 5
		w.Pressure -= 5
	} else {
		w.FlowRate -= 20
	}

	w.FlowRate = clamp(w.FlowRate, 0, 5000)
	w.Pressure = clamp(w.Pressure, 2500, 4000)
	w.Temperature = clamp(w.Temperature, 70, 100)
}

func (w *Well) updateShutIn() {
	w.FlowRate -= 100
	if w.FlowRate < 0 {
		w.FlowRate = 0
	}
	w.Pressure += 15
	if w.Pressure > 4000 {
		w.Pressure = 4000
	}
	w.Temperature += rand.Float64()*0.4 - 0.2
}

func (w *Well) updateFault() {
	// Rapid deterioration during a fault.
	w.FlowRate -= 200
	if w.FlowRate < 0 {
		w.FlowRate = 0
	}
	w.Pressure += 20
	w.Temperature += 1

	// Fault lets values exceed normal operating limits.
	if w.Pressure > 4500 {
		w.Pressure = 4500
	}
	if w.Temperature > 120 {
		w.Temperature = 120
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Telemetry ──────────────────────────────────────────────────────────────

func (w *Well) GetTelemetry() Telemetry {
	alerts, level := w.CheckAlerts()
	return Telemetry{
		Timestamp:   time.Now(),
		WellID:      w.ID,
		State:       w.State,
		Pressure:    w.Pressure,
		Temperature: w.Temperature,
		FlowRate:    w.FlowRate,
		ValveOpen:   w.ValveOpen,
		PumpRunning: w.PumpRunning,
		Alerts:      alerts,
		AlertLevel:  level,
	}
}

// ── HTTP / WebSocket ───────────────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Initialise well ONCE per connection — state persists across ticks.
	well := Well{
		ID:          "WELL-001",
		State:       Producing,
		Pressure:    3200,
		Temperature: 82,
		FlowRate:    2500,
		ValveOpen:   true,
		PumpRunning: true,
	}

	for {
		well.Update()
		telemetry := well.GetTelemetry()

		if err := conn.WriteJSON(telemetry); err != nil {
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
	http.HandleFunc("/well/telemetry", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Println("Request hits well telemetry")
	})
	// fmt.Println("Server started on http://localhost:8080/")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
