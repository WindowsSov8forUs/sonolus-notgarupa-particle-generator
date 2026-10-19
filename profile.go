package particle

// The converter's model of an original particle bundle: the Unity
// ParticleSystem components of its prefabs, with the fields named as in
// Unity's serialisation (they are decoded from the bundles' type trees).
// Only the fields the converter consumes are declared.

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/unity"
)

type Bundle struct {
	Key       string                     `json:"key"`
	Systems   []SystemDef                `json:"systems"`
	Profiles  map[string]SystemProfile   `json:"profiles"`
	Modules   ModuleProfiles             `json:"moduleProfiles"`
	Renderers map[string]RendererProfile `json:"rendererProfiles"`
	Materials []Material                 `json:"materials"`
	Textures  []TextureInfo              `json:"textures"`
	// Meshes are the bundle's particle meshes by name (render mode 4).
	Meshes map[string]*unity.Mesh `json:"-"`
}

type Vec3 struct{ X, Y, Z float64 }
type Quat struct{ X, Y, Z, W float64 }

type TransformProfile struct {
	Position Vec3 `json:"m_LocalPosition"`
	Rotation Quat `json:"m_LocalRotation"`
	Scale    Vec3 `json:"m_LocalScale"`
}

type SystemDef struct {
	Identity         string             `json:"identity"`
	Path             string             `json:"path"`
	Transform        TransformProfile   `json:"transform"`
	ParentTransforms []TransformProfile `json:"parentTransforms"`
	Profile          string             `json:"profile"`
}

type SystemProfile struct {
	System struct {
		LengthInSec     float64     `json:"lengthInSec"`
		SimulationSpeed float64     `json:"simulationSpeed"`
		Looping         bool        `json:"looping"`
		StartDelay      MinMaxCurve `json:"startDelay"`
		ScalingMode     int         `json:"scalingMode"`
	} `json:"system"`
	Modules  map[string]string `json:"modules"`
	Renderer string            `json:"renderer"`
}

type ModuleProfiles struct {
	Initial       map[string]InitialModule       `json:"InitialModule"`
	Emission      map[string]EmissionModule      `json:"EmissionModule"`
	Shape         map[string]ShapeModule         `json:"ShapeModule"`
	Color         map[string]ColorModule         `json:"ColorModule"`
	Size          map[string]SizeModule          `json:"SizeModule"`
	Rotation      map[string]RotationModule      `json:"RotationModule"`
	RotationSpeed map[string]RotationBySpeed     `json:"RotationBySpeedModule"`
	Clamp         map[string]ClampVelocityModule `json:"ClampVelocityModule"`
	UV            map[string]UVModule            `json:"UVModule"`
	Velocity      map[string]VelocityModule      `json:"VelocityModule"`
	Force         map[string]ForceModule         `json:"ForceModule"`
	CustomData    map[string]CustomDataModule    `json:"CustomDataModule"`
}

// Slope is a curve tangent that may be serialised as "number:±infinity".
type Slope float64

func (s Slope) MarshalJSON() ([]byte, error) {
	switch {
	case math.IsInf(float64(s), 1):
		return []byte(`"number:+infinity"`), nil
	case math.IsInf(float64(s), -1):
		return []byte(`"number:-infinity"`), nil
	}
	return json.Marshal(float64(s))
}

func (s *Slope) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), `"`)
	switch str {
	case "number:+infinity":
		*s = Slope(math.Inf(1))
	case "number:-infinity":
		*s = Slope(math.Inf(-1))
	default:
		var f float64
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		*s = Slope(f)
	}
	return nil
}

type Keyframe struct {
	Time     float64 `json:"time"`
	Value    float64 `json:"value"`
	InSlope  Slope   `json:"inSlope"`
	OutSlope Slope   `json:"outSlope"`
}

type AnimationCurve struct {
	Keys []Keyframe `json:"m_Curve"`
}

type MinMaxCurve struct {
	State     int            `json:"minMaxState"`
	Scalar    float64        `json:"scalar"`
	MinScalar float64        `json:"minScalar"`
	MaxCurve  AnimationCurve `json:"maxCurve"`
	MinCurve  AnimationCurve `json:"minCurve"`
}

type Color struct{ R, G, B, A float64 }

type Gradient struct {
	Keys  [8]Color
	CTime [8]float64
	ATime [8]float64
	Mode  int
	NC    int
	NA    int
}

func (g Gradient) MarshalJSON() ([]byte, error) {
	m := map[string]any{"m_Mode": g.Mode, "m_NumColorKeys": g.NC, "m_NumAlphaKeys": g.NA}
	for i := 0; i < 8; i++ {
		d := string(rune('0' + i))
		m["key"+d], m["ctime"+d], m["atime"+d] = g.Keys[i], g.CTime[i], g.ATime[i]
	}
	return json.Marshal(m)
}

func (g *Gradient) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for i := 0; i < 8; i++ {
		if v, ok := raw["key"+string(rune('0'+i))]; ok {
			_ = json.Unmarshal(v, &g.Keys[i])
		}
		if v, ok := raw["ctime"+string(rune('0'+i))]; ok {
			_ = json.Unmarshal(v, &g.CTime[i])
		}
		if v, ok := raw["atime"+string(rune('0'+i))]; ok {
			_ = json.Unmarshal(v, &g.ATime[i])
		}
	}
	_ = json.Unmarshal(raw["m_Mode"], &g.Mode)
	_ = json.Unmarshal(raw["m_NumColorKeys"], &g.NC)
	_ = json.Unmarshal(raw["m_NumAlphaKeys"], &g.NA)
	return nil
}

type MinMaxGradient struct {
	State       int      `json:"minMaxState"`
	MinColor    Color    `json:"minColor"`
	MaxColor    Color    `json:"maxColor"`
	MaxGradient Gradient `json:"maxGradient"`
	MinGradient Gradient `json:"minGradient"`
}

type InitialModule struct {
	Enabled         bool           `json:"enabled"`
	StartLifetime   MinMaxCurve    `json:"startLifetime"`
	StartSpeed      MinMaxCurve    `json:"startSpeed"`
	StartColor      MinMaxGradient `json:"startColor"`
	StartSize       MinMaxCurve    `json:"startSize"`
	StartSizeY      MinMaxCurve    `json:"startSizeY"`
	StartSizeZ      MinMaxCurve    `json:"startSizeZ"`
	StartRotation   MinMaxCurve    `json:"startRotation"`
	StartRotationX  MinMaxCurve    `json:"startRotationX"`
	StartRotationY  MinMaxCurve    `json:"startRotationY"`
	RandomizeRotDir float64        `json:"randomizeRotationDirection"`
	MaxNumParticles int            `json:"maxNumParticles"`
	Size3D          bool           `json:"size3D"`
	Rotation3D      bool           `json:"rotation3D"`
	GravityModifier MinMaxCurve    `json:"gravityModifier"`
}

type Burst struct {
	Time           float64     `json:"time"`
	Count          MinMaxCurve `json:"countCurve"`
	CycleCount     int         `json:"cycleCount"`
	RepeatInterval float64     `json:"repeatInterval"`
	Probability    float64     `json:"probability"`
}

type EmissionModule struct {
	Enabled bool        `json:"enabled"`
	Rate    MinMaxCurve `json:"rateOverTime"`
	Bursts  []Burst     `json:"m_Bursts"`
}

type ShapeValue struct {
	Value float64 `json:"value"`
	Mode  int     `json:"mode"`
}

type ShapeModule struct {
	Enabled          bool       `json:"enabled"`
	Type             int        `json:"type"`
	Angle            float64    `json:"angle"`
	Length           float64    `json:"length"`
	RadiusThickness  float64    `json:"radiusThickness"`
	Position         Vec3       `json:"m_Position"`
	Rotation         Vec3       `json:"m_Rotation"`
	Scale            Vec3       `json:"m_Scale"`
	Radius           ShapeValue `json:"radius"`
	Arc              ShapeValue `json:"arc"`
	RandomDirection  float64    `json:"randomDirectionAmount"`
	SphericalDir     float64    `json:"sphericalDirectionAmount"`
	RandomPosition   float64    `json:"randomPositionAmount"`
	AlignToDirection bool       `json:"alignToDirection"`
}

type ColorModule struct {
	Enabled  bool           `json:"enabled"`
	Gradient MinMaxGradient `json:"gradient"`
}

type SizeModule struct {
	Enabled      bool        `json:"enabled"`
	Curve        MinMaxCurve `json:"curve"`
	Y            MinMaxCurve `json:"y"`
	Z            MinMaxCurve `json:"z"`
	SeparateAxes bool        `json:"separateAxes"`
}

type RotationModule struct {
	Enabled      bool        `json:"enabled"`
	Curve        MinMaxCurve `json:"curve"` // z
	X            MinMaxCurve `json:"x"`
	Y            MinMaxCurve `json:"y"`
	SeparateAxes bool        `json:"separateAxes"`
}

type RotationBySpeed struct {
	Enabled      bool                   `json:"enabled"`
	Curve        MinMaxCurve            `json:"curve"` // z
	X            MinMaxCurve            `json:"x"`
	Y            MinMaxCurve            `json:"y"`
	SeparateAxes bool                   `json:"separateAxes"`
	Range        struct{ X, Y float64 } `json:"range"`
}

// VelocityModule is Velocity over Lifetime: linear velocity, orbital
// rotation around an offset and radial speed, all added to the motion of the
// frame without accumulating, and a speed modifier on the total velocity.
type VelocityModule struct {
	Enabled        bool `json:"enabled"`
	X, Y, Z        MinMaxCurve
	OrbitalX       MinMaxCurve `json:"orbitalX"`
	OrbitalY       MinMaxCurve `json:"orbitalY"`
	OrbitalZ       MinMaxCurve `json:"orbitalZ"`
	OrbitalOffsetX MinMaxCurve `json:"orbitalOffsetX"`
	OrbitalOffsetY MinMaxCurve `json:"orbitalOffsetY"`
	OrbitalOffsetZ MinMaxCurve `json:"orbitalOffsetZ"`
	Radial         MinMaxCurve `json:"radial"`
	SpeedModifier  MinMaxCurve `json:"speedModifier"`
	InWorldSpace   bool        `json:"inWorldSpace"`
}

// ForceModule is Force over Lifetime: an acceleration of the base velocity.
type ForceModule struct {
	Enabled           bool `json:"enabled"`
	X, Y, Z           MinMaxCurve
	InWorldSpace      bool `json:"inWorldSpace"`
	RandomizePerFrame bool `json:"randomizePerFrame"`
}

// CustomDataModule streams per-particle vectors to custom shaders (the
// UV-scroll shader reads the first vector).
type CustomDataModule struct {
	Enabled bool        `json:"enabled"`
	Mode0   int         `json:"mode0"` // 1 vector
	Count0  int         `json:"vectorComponentCount0"`
	V0      MinMaxCurve `json:"vector0_0"`
	V1      MinMaxCurve `json:"vector0_1"`
	V2      MinMaxCurve `json:"vector0_2"`
	V3      MinMaxCurve `json:"vector0_3"`
}

type ClampVelocityModule struct {
	Enabled        bool `json:"enabled"`
	X, Y, Z        MinMaxCurve
	Magnitude      MinMaxCurve `json:"magnitude"`
	SeparateAxis   bool        `json:"separateAxis"`
	InWorldSpace   bool        `json:"inWorldSpace"`
	DragBySize     bool        `json:"multiplyDragByParticleSize"`
	DragByVelocity bool        `json:"multiplyDragByParticleVelocity"`
	Dampen         float64     `json:"dampen"`
	Drag           MinMaxCurve `json:"drag"`
}

type UVModule struct {
	Enabled       bool        `json:"enabled"`
	Mode          int         `json:"mode"`
	FrameOverTime MinMaxCurve `json:"frameOverTime"`
	StartFrame    MinMaxCurve `json:"startFrame"`
	TilesX        int         `json:"tilesX"`
	TilesY        int         `json:"tilesY"`
	AnimationType int         `json:"animationType"`
	RowIndex      int         `json:"rowIndex"`
	Cycles        float64     `json:"cycles"`
}

type RendererProfile struct {
	Enabled       bool    `json:"m_Enabled"`
	RenderMode    int     `json:"m_RenderMode"`
	Alignment     int     `json:"m_RenderAlignment"`
	LengthScale   float64 `json:"m_LengthScale"`
	VelocityScale float64 `json:"m_VelocityScale"`
	SortingOrder  int     `json:"m_SortingOrder"`
	Pivot         Vec3    `json:"m_Pivot"`
	// Mesh is the mesh of render mode 4: meshQuad for Unity's built-in Quad,
	// otherwise the name of a mesh of the bundle.
	Mesh      string `json:"mesh,omitempty"`
	Materials []*struct {
		Name string `json:"name"`
	} `json:"m_Materials"`
}

type Material struct {
	Name             string                 `json:"name"`
	Shader           string                 `json:"shader"`
	Texture          string                 `json:"texture"`
	SourceBlend      int                    `json:"sourceBlendFactor"`
	DestinationBlend int                    `json:"destinationBlendFactor"`
	Fragment         string                 `json:"fragment"`
	TextureScale     struct{ X, Y float64 } `json:"mainTextureScale"`
	TextureOffset    struct{ X, Y float64 } `json:"mainTextureOffset"`
	// UVScroll: the UV-scroll shader adds custom vector 0 (y, x) to the UVs.
	UVScroll bool `json:"uvScroll,omitempty"`
}

type TextureInfo struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (b *Bundle) material(name string) *Material {
	for i := range b.Materials {
		if b.Materials[i].Name == name {
			return &b.Materials[i]
		}
	}
	return nil
}
