package node

import (
	"math"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.Satellite = (*LiveSatellite)(nil)

const (
	earthMu            = 3.986004418e14 // Standard gravitational parameter of Earth (m^3/s^2)
	earthEquatorialRad = 6378137.0      // WGS-84 equatorial radius (m)
	earthJ2            = 1.08262668e-3  // Second zonal harmonic coefficient (oblateness)
)

// LiveSatellite represents a satellite node that calculates its orbital mechanics in real-time.
type LiveSatellite struct {
	BaseNode

	inclination          float64
	inclinationRad       float64
	rightAscension       float64
	rightAscensionRad    float64
	eccentricity         float64
	argumentOfPerigee    float64
	argumentOfPerigeeRad float64
	meanAnomaly          float64
	meanMotion           float64
	semiMajorAxis        float64
	epoch                time.Time
	ISLProtocol          types.InterSatelliteLinkProtocol
	groundLinks          []types.Link

	// Control Plane fields
	coordinatingGS types.GroundStation
}

// NewLiveSatellite initializes a new LiveSatellite with orbital configuration.
func NewLiveSatellite(name string, inclination, raan, ecc, argPerigee, meanAnomaly, meanMotion float64, epoch time.Time, simTime time.Time, isl types.InterSatelliteLinkProtocol, router types.Router, computing types.Computing) *LiveSatellite {
	inclRad := types.DegreesToRadians(inclination)
	raanRad := types.DegreesToRadians(raan)
	argPerigeeRad := types.DegreesToRadians(argPerigee)

	// Calculate exact semi-major axis from mean motion via Kepler's Third Law: a = (mu / n^2)^(1/3)
	semiMajorAxis := 6921000.0 // Fallback ~550km LEO
	if meanMotion > 0 {
		nRadPerSec := meanMotion * 2.0 * math.Pi / 86400.0
		semiMajorAxis = math.Cbrt(earthMu / (nRadPerSec * nRadPerSec))
	}

	s := &LiveSatellite{
		BaseNode:             BaseNode{Name: name, Router: router, Computing: computing},
		inclination:          inclination,
		inclinationRad:       inclRad,
		rightAscension:       raan,
		rightAscensionRad:    raanRad,
		eccentricity:         ecc,
		argumentOfPerigee:    argPerigee,
		argumentOfPerigeeRad: argPerigeeRad,
		meanAnomaly:          meanAnomaly,
		meanMotion:           meanMotion,
		semiMajorAxis:        semiMajorAxis,
		epoch:                epoch,
		ISLProtocol:          isl,
		groundLinks:          []types.Link{},
	}

	isl.Mount(s)
	router.Mount(s)
	computing.Mount(s)
	s.UpdatePosition(simTime)
	return s
}

// UpdatePosition calculates the satellite's position using J2-perturbed Keplerian orbital mechanics.
func (s *LiveSatellite) UpdatePosition(simTime time.Time) {
	deltaT := simTime.Sub(s.epoch).Seconds() // Time since epoch in seconds
	n0 := s.meanMotion * 2.0 * math.Pi / 86400.0

	a := s.semiMajorAxis
	if a <= 0 {
		a = 6921000.0
	}

	// Secular J2 perturbations (Earth oblateness effects on RAAN, Argument of Perigee, and Mean Anomaly)
	p := a * (1.0 - s.eccentricity*s.eccentricity)
	cosI := math.Cos(s.inclinationRad)

	j2Factor := 1.5 * earthJ2 * (earthEquatorialRad * earthEquatorialRad) / (p * p) * n0
	raanDot := -j2Factor * cosI
	argPerigeeDot := 0.5 * j2Factor * (5.0*cosI*cosI - 1.0)
	meanAnomalyDot := n0 + 0.5*j2Factor*math.Sqrt(1.0-s.eccentricity*s.eccentricity)*(3.0*cosI*cosI-1.0)

	currentRAAN := normalizeAngle(s.rightAscensionRad + raanDot*deltaT)
	currentArgPerigee := normalizeAngle(s.argumentOfPerigeeRad + argPerigeeDot*deltaT)
	meanAnomalyCurrent := normalizeAngle(s.meanAnomaly + meanAnomalyDot*deltaT)

	eccentricAnomaly := solveKeplersEquation(meanAnomalyCurrent, s.eccentricity)
	trueAnomaly := computeTrueAnomaly(eccentricAnomaly, s.eccentricity)

	distance := a * (1.0 - s.eccentricity*math.Cos(eccentricAnomaly))
	xp := distance * math.Cos(trueAnomaly)
	yp := distance * math.Sin(trueAnomaly)
	zp := 0.0

	s.Position = applyOrbitalTransformations(xp, yp, zp, s.inclinationRad, currentArgPerigee, currentRAAN)
}

func (s *LiveSatellite) GetLinkNodeProtocol() types.LinkNodeProtocol {
	return s.ISLProtocol
}

func (s *LiveSatellite) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return s.ISLProtocol
}

// SetCoordinatingGS sets the logical controller for this live satellite.
func (s *LiveSatellite) SetCoordinatingGS(gs types.GroundStation) {
	s.coordinatingGS = gs
}

// GetCoordinatingGS returns the logical controller.
func (s *LiveSatellite) GetCoordinatingGS() types.GroundStation {
	return s.coordinatingGS
}

// ApplyOrbitalTransformations converts orbital plane coordinates into the Earth-Centered frame
func applyOrbitalTransformations(x, y, z, iRad, omegaRad, raanRad float64) types.Vector {
	cosRAAN := math.Cos(raanRad)
	sinRAAN := math.Sin(raanRad)
	cosIncl := math.Cos(iRad)
	sinIncl := math.Sin(iRad)
	cosArgP := math.Cos(omegaRad)
	sinArgP := math.Sin(omegaRad)

	xECI := (cosRAAN*cosArgP-sinRAAN*sinArgP*cosIncl)*x + (-cosRAAN*sinArgP-sinRAAN*cosArgP*cosIncl)*y
	yECI := (sinRAAN*cosArgP+cosRAAN*sinArgP*cosIncl)*x + (-sinRAAN*sinArgP+cosRAAN*cosArgP*cosIncl)*y
	zECI := sinIncl*sinArgP*x + sinIncl*cosArgP*y

	return types.Vector{X: xECI, Y: yECI, Z: zECI}
}

// normalizeAngle wraps an angle in radians into the range [0, 2π].
func normalizeAngle(rad float64) float64 {
	rad = math.Mod(rad, 2*math.Pi)
	if rad < 0 {
		rad += 2 * math.Pi
	}
	return rad
}

// solveKeplersEquation uses high-precision Newton-Raphson iteration to solve for the eccentric anomaly.
func solveKeplersEquation(meanAnomaly, ecc float64) float64 {
	E := meanAnomaly
	tol := 1e-10
	for iter := 0; iter < 30; iter++ {
		delta := (E - ecc*math.Sin(E) - meanAnomaly) / (1.0 - ecc*math.Cos(E))
		E -= delta
		if math.Abs(delta) <= tol {
			break
		}
	}
	return E
}

// computeTrueAnomaly calculates the true anomaly from the eccentric anomaly.
func computeTrueAnomaly(E, ecc float64) float64 {
	sqrt1me2 := math.Sqrt(1 - ecc*ecc)
	return math.Atan2(sqrt1me2*math.Sin(E), math.Cos(E)-ecc)
}
