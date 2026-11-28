// Command antena-batch is one run of the batch that writes
// https://prpr-antena.com/ (the Go port of migration-next/bin/run.rb). It
// reads the same environment variables as the Ruby runner.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/jiikko/antena/batch/internal/runner"
	"github.com/jiikko/antena/batch/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migration-next failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	// Like Ruby's env.fetch: a variable set to "" is used as "", not replaced.
	region, ok := os.LookupEnv("AWS_REGION")
	if !ok {
		region = "ap-northeast-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return err
	}
	st, err := store.FromEnv(os.Getenv, os.LookupEnv, s3.NewFromConfig(cfg))
	if err != nil {
		return err
	}
	sites, ok := os.LookupEnv("MIGRATION_NEXT_SITES_FILE")
	if !ok {
		sites = "config/sites.yml"
	}
	r := &runner.Runner{Store: st, SitesFile: sites, Fetch: runner.HTTPFetch(), Now: time.Now, Log: os.Stdout, Err: os.Stderr}
	return r.Execute(ctx)
}
