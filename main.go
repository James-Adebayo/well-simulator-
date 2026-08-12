package main

import (
	"time"
)

type Well struct {
	ID          string
	Pressure    float64
	Temperature float64
	FlowRate    float64
	ValveOpen   bool
	PumpRunning bool
}

type Telementry struct {
	Timestamp   time.Time `json:"timestamp"`
	WellID      string    `json:"well_id"`
	Pressure    float64   `json:"pressure_psi"`
	Temperature float64   `json:"temperature_c"`
	FlowRate    float64   `json:"flow_rate_bpd"`
	ValveOpen   bool      `json:"valve_open"`
	PumpRunning bool      `json:"pump_running"`
}

func (w *Well) update() {
	//  I want to create a small natural fluntation first

}
