// Command sonolus-notgarupa-particle-generator builds the NotGarupa particle
// pack (see package particle) as a sonolus-pack source tree, a servable pack
// and the effect contract.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	particle "github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/cli"
)

var version = "dev"

func main() {
	var common cli.Common
	fs := flag.NewFlagSet("sonolus-notgarupa-particle-generator", flag.ExitOnError)
	common.Register(fs)
	var o particle.Options
	fs.IntVar(&o.Strata, "strata", 4, "stratified sub-groups per burst for randomised values that Sonolus cannot express linearly")
	fs.IntVar(&o.Segments, "segments", 8, "maximum time segments per particle")
	fs.Float64Var(&o.Tolerance, "tolerance", 0.05, "linear-interpolation tolerance in particle units")
	fs.Float64Var(&o.LoopSeconds, "loop", 1, "period of looping effects (effect_TapKeep) in seconds")
	standard := fs.Bool("standard", true, "also emit standard Sonolus effect names as copies of the matching prefabs")
	fs.StringVar(&o.Name, "name", "", "particle item name (default notgarupa-skin00; habahiro: notgarupa-habahiro)")
	contract := fs.String("contract", "contract.json", "write the effect contract JSON here (empty: skip; habahiro: contract-habahiro.json)")
	variant := fs.String("variant", "normal", "normal (tap effect skin00) or habahiro (HABAHIRO tap effects with width variants, for multi-width charts)")
	assetVersion := fs.String("asset-version", particle.PinnedAssetVersion, "content CDN asset version of the tap-effect bundles")
	_ = fs.Parse(os.Args[1:])
	if common.Version {
		fmt.Println(version)
		return
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	o.Variant = particle.Variant(*variant)
	if o.Variant == particle.Habahiro {
		// Separate outputs so that the normal pack stays untouched.
		defaults := map[string]*string{"contract": contract, "out": &common.Out, "pack": &common.Pack}
		values := map[string]string{"contract": "contract-habahiro.json", "out": "out/source-habahiro", "pack": "out/pack-habahiro"}
		for k, p := range defaults {
			if !explicit[k] {
				*p = values[k]
			}
		}
	}
	o.NoStandardNames = !*standard
	o.Source = particle.Source{CacheDir: common.Cache, AssetVersion: *assetVersion, Offline: common.Offline, Log: common.Logf()}

	r, err := particle.Generate(context.Background(), o)
	if err != nil {
		cli.Fatal(err)
	}
	if err := particle.WriteSource(common.Out, r); err != nil {
		cli.Fatal(err)
	}
	if *contract != "" {
		if err := cli.WriteJSON(*contract, r.Contract); err != nil {
			cli.Fatal(err)
		}
	}
	if common.Pack != "" {
		if err := cli.Pack(common.Out, common.Pack); err != nil {
			cli.Fatal(err)
		}
	}
	for _, m := range r.Contract.Effects {
		fmt.Printf("%-48s %5.2fs loop=%-5v groups=%3d particles=%4d\n", m.Name, m.Duration, m.Loop, m.Groups, m.Particles)
	}
	fmt.Printf("particle %s: %d effects, %d sprites, texture %dx%d -> %s\n", r.Item.Name, len(r.Data.Effects), len(r.Data.Sprites),
		r.Contract.Texture[0], r.Contract.Texture[1], common.Out)
}
