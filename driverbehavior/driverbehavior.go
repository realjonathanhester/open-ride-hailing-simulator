// Package driverbehavior holds the built-in driver behavior plugins.
package driverbehavior

import "github.com/realjonathanhester/ride-hailing-simulator/model"

// Plugin names used in [hooks] driver_behavior = "...".
const (
	SimpleName         = "simple"
	ReturnToOriginName = "return_to_origin"
)

// Simple is the default: the driver accepts every assignment, goes online
// and offline once as the supply input says, and stays still when idle.
type Simple struct{}

// OnAssignmentOffered accepts every plan change.
func (Simple) OnAssignmentOffered(model.DriverState, []model.Stop) bool { return true }

// OnIdle never repositions.
func (Simple) OnIdle(model.DriverState) *model.Location { return nil }

// ReturnToOrigin behaves like Simple, except that whenever the car becomes
// idle away from its supply online location it drives back there.
type ReturnToOrigin struct{ Simple }

// OnIdle returns the online location unless the car is already there.
func (ReturnToOrigin) OnIdle(car model.DriverState) *model.Location {
	if car.Location == car.OnlineLocation {
		return nil
	}
	target := car.OnlineLocation
	return &target
}
