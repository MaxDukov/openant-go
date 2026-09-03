// Package devices implements ready-made ANT+ device profiles, an RF
// scanner and utility conversions.
//
// It is the Go equivalent of openant.devices (github.com/Tigge/openant).
//
// # Profiles
//
// Each profile wraps an easy.Channel with page decoding and exposes typed
// accessors. A profile connects to a sensor by device type (and optionally
// a specific device number):
//
//	n, _ := easy.New()
//	d, _ := devices.NewHeartRate(n, 0) // 0 = first sensor found
//	d.OnData = func(data devices.DeviceData) {
//	    hr := data.(devices.HeartRateData)
//	    fmt.Println(hr.BeatCount, hr.ComputedHeartRate)
//	}
//	d.Start()
//	// ... later
//	d.Stop()
//
// Available profiles: BikeCadence, BikeSpeed, BikeSpeedCadence, Control,
// Environment, FitnessEquipment, HeartRate, Lev, PowerMeter, Shift,
// DropperSeatpost, TirePressureMonitor, StrideSpeedDistance, WeightScale.
//
// To attach to a specific device, pass its ID to the constructor and wait
// for it to appear with [baseDevice.WaitFound]:
//
//	d, _ := devices.NewHeartRate(n, 41234, 0)
//	if err := d.WaitFound(30 * time.Second); err != nil {
//	    // sensor with that ID never showed up
//	}
//
// Some profiles can also act as the sensor: NewHeartRateMaster emulates
// an ANT+ heart rate belt (set the bpm with SetHeartRate), and
// NewFitnessEquipment exposes a controllable trainer interface
// (SetTargetPower, SetTrackResistance, SetUserConfig, workouts).
//
// # Scanner
//
// [Scanner] listens on all device types at once ([NewScanner]) and reports
// every appearing sensor with its identity; results can be persisted with
// [Scanner.Save] and loaded with [ReadJSONDevices]. [AutoCreateDevice]
// turns a scan hit into the matching profile.
//
// # Data export
//
// [ToInfluxJSON] converts any [DeviceData] into a tag/field map suitable
// for line-protocol writers; the (deferred) influx CLI builds on it.
package devices
