package ground_test

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/ground"
	"github.com/polaris-slo-cloud/stardust-go/internal/routing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

func TestGroundStationBuilderAndPosition(t *testing.T) {
	epoch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	routerBuilder := routing.NewRouterBuilder(configs.RouterConfig{
		Protocol: "a-star",
	})
	computingConfig := []configs.ComputingConfig{
		{
			Type:   types.Cloud,
			Cores:  16,
			Memory: 64000,
		},
		{
			Type:   types.Edge,
			Cores:  4,
			Memory: 16000,
		},
	}
	compBuilder := computing.NewComputingBuilder(computingConfig)
	groundLinkConfig := configs.GroundLinkConfig{
		Protocol: "nearest",
	}

	builder := ground.NewGroundStationBuilder(epoch, routerBuilder, compBuilder, groundLinkConfig)
	gs := builder.SetName("Budapest").
		SetLatitude(47.4979).
		SetLongitude(19.0402).
		SetComputingType("Cloud").
		Build()

	if gs.GetName() != "Budapest" {
		t.Errorf("expected name Budapest, got %s", gs.GetName())
	}
	if gs.GetComputing().GetComputingType() != types.Cloud {
		t.Errorf("expected computing type Cloud, got %v", gs.GetComputing().GetComputingType())
	}

	pos := gs.GetPosition()
	// Budapest is at lat ~47.5 deg N, lon ~19.0 deg E
	// Radius from Earth center should be approx Earth Radius (6371 km)
	r := pos.Magnitude()
	if r < 6350000.0 || r > 6380000.0 {
		t.Errorf("expected radius ~6371km, got %f m", r)
	}

	// At lat ~47.5 deg, Z should be positive: ~6378 * sin(47.5 deg) ~ 4680 km
	if pos.Z < 4500000.0 || pos.Z > 4800000.0 {
		t.Errorf("expected Z coordinate in ~4600-4750km range, got %f", pos.Z)
	}

	// At lon ~19 deg, both X and Y should be positive (Eastern hemisphere, near prime meridian)
	if pos.X <= 0 || pos.Y <= 0 {
		t.Errorf("expected X > 0 and Y > 0 for Budapest, got X=%f, Y=%f", pos.X, pos.Y)
	}

	// Verify longitude angle atan2(Y, X) ~ 19.04 degrees
	lonDeg := math.Atan2(pos.Y, pos.X) * 180.0 / math.Pi
	if math.Abs(lonDeg-19.0402) > 0.1 {
		t.Errorf("expected longitude ~19.04 deg, got %f deg", lonDeg)
	}
}
