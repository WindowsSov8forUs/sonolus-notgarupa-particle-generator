// Command sonolus-notgarupa-particle-generator builds the NotGarupa particle
// pack (see package particle): the ordinary item and the #HABAHIRO item as one
// sonolus-pack source tree and servable pack, plus their effect contracts.
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
	fs.StringVar(&o.Name, "name", "notgarupa-skin00", "ordinary particle item name")
	fs.StringVar(&o.HabahiroName, "name-habahiro", "notgarupa-habahiro", "#HABAHIRO particle item name")
	contract := fs.String("contract", "contract.json", "write the ordinary item's effect contract JSON here (empty: skip)")
	habahiroContract := fs.String("contract-habahiro", "contract-habahiro.json", "write the HABAHIRO item's effect contract JSON here (empty: skip)")
	assetVersion := fs.String("asset-version", particle.PinnedAssetVersion, "content CDN asset version of the tap-effect bundles")
	_ = fs.Parse(os.Args[1:])
	if common.Version {
		fmt.Println(version)
		return
	}
	o.NoStandardNames = !*standard
	o.Source = particle.Source{CacheDir: common.Cache, AssetVersion: *assetVersion, Offline: common.Offline, Log: common.Logf()}

	results, err := particle.Generate(context.Background(), o)
	if err != nil {
		cli.Fatal(err)
	}
	if err := particle.WriteSource(common.Out, results...); err != nil {
		cli.Fatal(err)
	}
	for _, r := range results {
		path := *contract
		if r.Habahiro {
			path = *habahiroContract
		}
		if path == "" {
			continue
		}
		if err := cli.WriteJSON(path, r.Contract); err != nil {
			cli.Fatal(err)
		}
	}
	if common.Pack != "" {
		if err := cli.Pack(common.Out, common.Pack); err != nil {
			cli.Fatal(err)
		}
	}
	for _, r := range results {
		fmt.Printf("== %s\n", r.Item.Name)
		for _, m := range r.Contract.Effects {
			fmt.Printf("%-48s %5.2fs loop=%-5v groups=%3d particles=%4d\n", m.Name, m.Duration, m.Loop, m.Groups, m.Particles)
		}
		fmt.Printf("particle %s: %d effects, %d sprites, texture %dx%d\n", r.Item.Name, len(r.Data.Effects), len(r.Data.Sprites),
			r.Contract.Texture[0], r.Contract.Texture[1])
	}
	fmt.Printf("%d particles -> %s\n", len(results), common.Out)
}
