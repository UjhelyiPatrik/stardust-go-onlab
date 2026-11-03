package types_test

import (
	"math"
	"testing"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

func TestVectorCreationAndEquality(t *testing.T) {
	v1 := types.NewVector(1.5, -2.5, 3.0)
	v2 := types.Vector{X: 1.5, Y: -2.5, Z: 3.0}
	v3 := types.NewVector(1.5, -2.5, 3.1)

	if !v1.Equals(v2) {
		t.Errorf("expected v1.Equals(v2) to be true, got false")
	}
	if v1.Equals(v3) {
		t.Errorf("expected v1.Equals(v3) to be false, got true")
	}
}

func TestVectorAbsAndMagnitude(t *testing.T) {
	tests := []struct {
		name     string
		v        types.Vector
		expected float64
	}{
		{
			name:     "Zero vector",
			v:        types.NewVector(0, 0, 0),
			expected: 0.0,
		},
		{
			name:     "Pythagorean 3-4-0",
			v:        types.NewVector(3, 4, 0),
			expected: 5.0,
		},
		{
			name:     "3D diagonal 1-2-2",
			v:        types.NewVector(1, 2, 2),
			expected: 3.0,
		},
		{
			name:     "Negative coordinates",
			v:        types.NewVector(-3, 0, -4),
			expected: 5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			abs := tt.v.Abs()
			mag := tt.v.Magnitude()

			if math.Abs(abs-tt.expected) > 1e-9 {
				t.Errorf("Abs(): expected %f, got %f", tt.expected, abs)
			}
			if math.Abs(mag-tt.expected) > 1e-9 {
				t.Errorf("Magnitude(): expected %f, got %f", tt.expected, mag)
			}
		})
	}
}

func TestVectorDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		v1       types.Vector
		v2       types.Vector
		expected float64
	}{
		{
			name:     "Orthogonal vectors",
			v1:       types.NewVector(1, 0, 0),
			v2:       types.NewVector(0, 1, 0),
			expected: 0.0,
		},
		{
			name:     "Parallel vectors",
			v1:       types.NewVector(2, 3, 4),
			v2:       types.NewVector(2, 3, 4),
			expected: 2*2 + 3*3 + 4*4, // 29
		},
		{
			name:     "Anti-parallel vectors",
			v1:       types.NewVector(1, 0, 0),
			v2:       types.NewVector(-1, 0, 0),
			expected: -1.0,
		},
		{
			name:     "Arbitrary vectors",
			v1:       types.NewVector(1.5, 2.0, -3.0),
			v2:       types.NewVector(4.0, -1.0, 2.5),
			expected: 1.5*4.0 + 2.0*(-1.0) + (-3.0)*2.5, // 6.0 - 2.0 - 7.5 = -3.5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dot := tt.v1.Dot(tt.v2)
			if math.Abs(dot-tt.expected) > 1e-9 {
				t.Errorf("Dot(): expected %f, got %f", tt.expected, dot)
			}
			// Dot product is commutative: v1 . v2 == v2 . v1
			revDot := tt.v2.Dot(tt.v1)
			if math.Abs(revDot-tt.expected) > 1e-9 {
				t.Errorf("Dot() commutativity: expected %f, got %f", tt.expected, revDot)
			}
		})
	}
}

func TestVectorCrossProduct(t *testing.T) {
	// Standard basis vectors
	i := types.NewVector(1, 0, 0)
	j := types.NewVector(0, 1, 0)
	k := types.NewVector(0, 0, 1)

	// i x j = k
	ixj := i.Cross(j)
	if !ixj.Equals(k) {
		t.Errorf("i x j: expected %v, got %v", k, ixj)
	}

	// j x i = -k
	jxi := j.Cross(i)
	negK := types.NewVector(0, 0, -1)
	if !jxi.Equals(negK) {
		t.Errorf("j x i: expected %v, got %v", negK, jxi)
	}

	// j x k = i
	jxk := j.Cross(k)
	if !jxk.Equals(i) {
		t.Errorf("j x k: expected %v, got %v", i, jxk)
	}

	// k x i = j
	kxi := k.Cross(i)
	if !kxi.Equals(j) {
		t.Errorf("k x i: expected %v, got %v", j, kxi)
	}

	// Parallel vectors cross product is zero
	par := i.Cross(types.NewVector(5, 0, 0))
	if !par.Equals(types.NewVector(0, 0, 0)) {
		t.Errorf("cross product of parallel vectors expected (0,0,0), got %v", par)
	}

	// Arbitrary cross product: (1, 2, 3) x (4, 5, 6) = (-3, 6, -3)
	v1 := types.NewVector(1, 2, 3)
	v2 := types.NewVector(4, 5, 6)
	expected := types.NewVector(
		2*6 - 3*5, // 12 - 15 = -3
		3*4 - 1*6, // 12 - 6 = 6
		1*5 - 2*4, // 5 - 8 = -3
	)
	res := v1.Cross(v2)
	if !res.Equals(expected) {
		t.Errorf("v1 x v2: expected %v, got %v", expected, res)
	}
}

func TestVectorNormalize(t *testing.T) {
	t.Run("Zero vector normalize returns zero vector", func(t *testing.T) {
		zero := types.NewVector(0, 0, 0)
		norm := zero.Normalize()
		if !norm.Equals(types.NewVector(0, 0, 0)) {
			t.Errorf("expected zero vector when normalizing (0,0,0), got %v", norm)
		}
	})

	t.Run("Non-zero vector normalize has unit length", func(t *testing.T) {
		v := types.NewVector(0, 3, 4)
		norm := v.Normalize()
		expected := types.NewVector(0, 3.0/5.0, 4.0/5.0)

		if math.Abs(norm.X-expected.X) > 1e-9 ||
			math.Abs(norm.Y-expected.Y) > 1e-9 ||
			math.Abs(norm.Z-expected.Z) > 1e-9 {
			t.Errorf("expected %v, got %v", expected, norm)
		}

		if math.Abs(norm.Magnitude()-1.0) > 1e-9 {
			t.Errorf("expected normalized vector magnitude to be 1.0, got %f", norm.Magnitude())
		}
	})
}

func TestVectorSubtract(t *testing.T) {
	// Note: In types.Vector, v.Subtract(other) returns (other - v)
	v := types.NewVector(10, 20, 30)
	other := types.NewVector(3, 5, 7)

	diff := v.Subtract(other)
	expected := types.NewVector(other.X-v.X, other.Y-v.Y, other.Z-v.Z) // -7, -15, -23

	if !diff.Equals(expected) {
		t.Errorf("Subtract(): expected %v, got %v", expected, diff)
	}
}

func TestDegreesToRadians(t *testing.T) {
	tests := []struct {
		deg      float64
		expected float64
	}{
		{0.0, 0.0},
		{90.0, math.Pi / 2.0},
		{180.0, math.Pi},
		{270.0, 3.0 * math.Pi / 2.0},
		{360.0, 2.0 * math.Pi},
		{-90.0, -math.Pi / 2.0},
		{45.0, math.Pi / 4.0},
	}

	for _, tt := range tests {
		rad := types.DegreesToRadians(tt.deg)
		if math.Abs(rad-tt.expected) > 1e-9 {
			t.Errorf("DegreesToRadians(%f): expected %f, got %f", tt.deg, tt.expected, rad)
		}
	}
}
