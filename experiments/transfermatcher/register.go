package transfermatcher

import (
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

func init() {
	if err := runner.RegisterMatcher(Name, func() (model.Matcher, error) {
		return New(), nil
	}); err != nil {
		panic(err)
	}
}
