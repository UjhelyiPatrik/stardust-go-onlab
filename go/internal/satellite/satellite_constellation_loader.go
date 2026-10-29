// File: internal/satellite/satellite_constellation_loader.go
// Handles registration and parsing of satellite constellation data (e.g., from TLE files)

package satellite

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// SatelliteConstellationLoader manages data source loaders (e.g., TLE) and loads satellite data.
type SatelliteConstellationLoader struct {
	loaders map[string]SatelliteDataSourceLoader // maps file type -> loader
}

// NewSatelliteConstellationLoader creates a loader registry for satellite sources (e.g., TLE).
func NewSatelliteConstellationLoader() *SatelliteConstellationLoader {
	return &SatelliteConstellationLoader{
		loaders: make(map[string]SatelliteDataSourceLoader),
	}
}

// RegisterDataSourceLoader allows plugging in different formats like TLE.
func (s *SatelliteConstellationLoader) RegisterDataSourceLoader(sourceType string, loader SatelliteDataSourceLoader) {
	s.loaders[sourceType] = loader
}

// LoadSatelliteConstellation loads and parses satellites using a registered loader.
func (s *SatelliteConstellationLoader) LoadSatelliteConstellation(dataSource string, sourceType string) ([]types.Satellite, error) {
	log.Printf("Loading satellite constellation from %s (%s)", dataSource, sourceType)

	reader, err := openDataSource(dataSource)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	loader, ok := s.loaders[sourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported data source type: %s", sourceType)
	}

	satellites, err := loader.Load(reader)
	if err != nil {
		return nil, err
	}

	// Constellation awareness (skip O(N^2) pre-allocation if protocol supports lazy link evaluation)
	if len(satellites) > 0 {
		if lazy, ok := satellites[0].GetISLProtocol().(interface{ SupportsLazyLinks() bool }); !ok || !lazy.SupportsLazyLinks() {
			for i, sat := range satellites {
				configureConstellation(sat, satellites[i+1:])
			}
		}
	}
	log.Printf("Loaded %d satellites", len(satellites))
	return satellites, nil
}

// configureConstellation links a satellite with all subsequent satellites in the constellation.
func configureConstellation(s types.Satellite, satellites []types.Satellite) {
	sProto := s.GetISLProtocol()
	for _, satellite := range satellites {
		if satellite == s {
			continue
		}
		link := linktypes.NewIslLink(s, satellite)
		sProto.AddLink(link)
		satellite.GetISLProtocol().AddLink(link)
	}
}

// openDataSource opens a local file or remote URL.
func openDataSource(dataSource string) (io.ReadCloser, error) {
	if strings.HasPrefix(dataSource, "http://") || strings.HasPrefix(dataSource, "https://") {
		resp, err := http.Get(dataSource)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}

	file, err := os.Open(dataSource)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// SatelliteDataSourceLoader is implemented by TLELoader or other sources.
// It parses satellite definitions from an input stream.
type SatelliteDataSourceLoader interface {
	Load(io.Reader) ([]types.Satellite, error)
}
