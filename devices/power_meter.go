package devices

import (
	"math"

	"github.com/maxdukov/openant-go/easy"
)

// PowerData is the ANT+ power data (openant PowerData).
type PowerData struct {
	InstantaneousPower int     `influx:"instantaneous_power"` // W
	AveragePower       int     `influx:"average_power"`       // W
	LeftPower          int     `influx:"left_power"`          // W
	RightPower         int     `influx:"right_power"`         // W
	Torque             float64 `influx:"torque"`              // Nm
	AngularVelocity    float64 `influx:"angular_velocity"`    // rad/s
	Cadence            int     `influx:"cadence"`             // rpm
}

// DataName implements DeviceData.
func (PowerData) DataName() string { return "PowerData" }

// PowerMeter is the ANT+ power meter profile (device type 11).
type PowerMeter struct {
	baseDevice
	Data PowerData

	powerUpdateEventCount  [2]int
	accumulatedPower       [2]int
	torqueUpdateEventCount [2]int
	crankTicks             [2]int
	accumulatedTorque      [2]int
	crankPeriod            [2]int
}

// NewPowerMeter creates the profile on the given node.
func NewPowerMeter(node *easy.Node, deviceID int, transType int) (*PowerMeter, error) {
	p := &PowerMeter{}
	p.node = node
	p.log = defaultLogger()
	p.DeviceType = int(DeviceTypePowerMeter)
	p.DeviceID = deviceID
	p.Period = 8182
	p.RFFreq = 57
	p.Name = "power_meter"
	p.TransType = transType
	p.Data = PowerData{LeftPower: -1, RightPower: -1, Cadence: 255}
	p.onProfileData = p.onData
	if err := p.openChannel(true, 0); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PowerMeter) onData(data []byte) {
	if len(data) < 8 {
		return
	}
	page := data[0]
	switch page {
	case 0x10: // standard power
		p.powerUpdateEventCount[0] = p.powerUpdateEventCount[1]
		p.powerUpdateEventCount[1] = int(data[1])
		p.accumulatedPower[0] = p.accumulatedPower[1]
		p.accumulatedPower[1] = int(data[4]) + int(data[5])<<8

		p.Data.Cadence = int(data[3])
		p.Data.InstantaneousPower = int(data[6]) + int(data[7])<<8

		// Pedal power bit 7 marks dual sided with RH percentage.
		if data[2]&(1<<7) != 0 && data[2] != 0xFF {
			percent := int(data[2] ^ (1 << 7))
			p.Data.RightPower = p.Data.InstantaneousPower * percent / 100
			p.Data.LeftPower = p.Data.InstantaneousPower - p.Data.RightPower
		}

		deltaUpdateCount := (p.powerUpdateEventCount[1] + 256 - p.powerUpdateEventCount[0]) % 256
		if deltaUpdateCount != 0 {
			p.Data.AveragePower = ((p.accumulatedPower[1] + 65536 - p.accumulatedPower[0]) % 65536) / deltaUpdateCount
			p.log.Info("standard power update", "device", p.String(),
				"power", p.Data.InstantaneousPower, "average", p.Data.AveragePower,
				"cadence", p.Data.Cadence)
			p.fireDeviceData(int(page), "standard_power", p.Data)
		}

	case 0x12: // standard torque
		p.torqueUpdateEventCount[0] = p.torqueUpdateEventCount[1]
		p.torqueUpdateEventCount[1] = int(data[1])
		p.crankTicks[0] = p.crankTicks[1]
		p.crankTicks[1] = int(data[2])
		p.crankPeriod[0] = p.crankPeriod[1]
		p.crankPeriod[1] = int(data[4]) + int(data[5])<<8
		p.accumulatedTorque[0] = p.accumulatedTorque[1]
		p.accumulatedTorque[1] = int(data[6]) + int(data[7])<<8

		p.Data.Cadence = int(data[3])

		deltaUpdateCount := (p.torqueUpdateEventCount[1] + 256 - p.torqueUpdateEventCount[0]) % 256
		deltaTorque := (p.accumulatedTorque[1] + 65536 - p.accumulatedTorque[0]) % 65536
		deltaCrankPeriod := p.crankPeriod[1] + 65536 - p.crankPeriod[0]%65536

		if deltaUpdateCount != 0 {
			p.Data.Torque = float64(deltaTorque) / (32 * float64(deltaUpdateCount))
			if deltaCrankPeriod != 0 {
				p.Data.AngularVelocity = (2 * math.Pi * float64(deltaUpdateCount)) / (float64(deltaCrankPeriod) / 2048)
			} else {
				p.Data.AngularVelocity = 0
			}
			p.Data.AveragePower = int(p.Data.Torque * p.Data.AngularVelocity)
			p.log.Info("standard torque update", "device", p.String(),
				"power", p.Data.AveragePower,
				"angular_velocity", p.Data.AngularVelocity,
				"torque", p.Data.Torque)
			p.fireDeviceData(int(page), "standard_torque", p.Data)
		}
	}
}

func init() {
	registerProfile(DeviceTypePowerMeter, func(node *easy.Node, deviceID, transType int) (any, error) {
		return NewPowerMeter(node, deviceID, transType)
	})
}
