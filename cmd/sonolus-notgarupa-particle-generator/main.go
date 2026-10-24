// Command sonolus-notgarupa-particle-generator builds every NotGarupa particle
// item (see package particle) as one sonolus-pack source tree, a servable pack
// and one effect contract per item.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	particle "github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/cli"
)

var version = "dev"

func main() {
	var common cli.Common
	fs := flag.NewFlagSet("sonolus-notgarupa-particle-generator", flag.ExitOnError)
	common.Register(fs)
	assetVersion := fs.String("asset-version", particle.PinnedAssetVersion, "content CDN asset version of the tap-effect bundles")
	_ = fs.Parse(os.Args[1:])
	if common.Version {
		fmt.Println(version)
		return
	}
	start := time.Now()
	source, pack, contracts := filepath.Join(common.Out, "source"), filepath.Join(common.Out, "pack"), filepath.Join(common.Out, "contracts")
	tree, err := particle.CreateSourceTree(source)
	if err != nil {
		cli.Fatal(err)
	}
	if err := os.RemoveAll(contracts); err != nil {
		cli.Fatal(err)
	}
	count := 0
	err = particle.Generate(context.Background(), particle.Options{
		Source:  particle.Source{CacheDir: common.Cache, AssetVersion: *assetVersion, Offline: common.Offline, Log: common.Logf()},
		Workers: common.Workers,
	}, func(r *particle.Result) error {
		if err := tree.Add(r); err != nil {
			return err
		}
		count++
		if common.Verbose {
			fmt.Fprintf(os.Stderr, "particle %s: %d effects, texture %dx%d\n", r.Item.Name, len(r.Data.Effects), r.Contract.Texture[0], r.Contract.Texture[1])
		}
		return cli.WriteJSON(filepath.Join(contracts, r.Item.Name+".json"), r.Contract)
	})
	if err != nil {
		cli.Fatal(err)
	}
	order, err := particle.Items()
	if err != nil {
		cli.Fatal(err)
	}
	if err := cli.Pack(source, pack, order); err != nil {
		cli.Fatal(err)
	}
	fmt.Printf("%d particle items -> %s (%s)\n", count, common.Out, time.Since(start).Round(time.Second))
}
