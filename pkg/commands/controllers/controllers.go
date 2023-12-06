package controllers

import (
	"fmt"
	"github.com/ekristen/fides/pkg/commands/global"
	"github.com/ekristen/fides/pkg/common"
	"github.com/ekristen/fides/pkg/controller"
	"github.com/urfave/cli/v2"
)

func Execute(c *cli.Context) error {
	if !c.Bool("agree-tos") {
		return fmt.Errorf("you must agree to the terms of service to use this, you can do this with --agree-tos")
	}

	// TODO: check for cluster values

	cfg := &oidc.Config{
		KubeConfigPath: c.String("kubeconfig"),
		Namespace:      c.String("namespace"),
		Lockname:       c.String("lockname"),
		BaseURL:        c.String("base-url"),
		SecretName:     c.String("secret-name"),
		ClusterKey:     c.String("cluster-key"),
		ClusterName:    c.String("cluster-name"),
		ClusterID:      c.String("cluster-id"),
	}

	return oidc.NewController(c.Context, cfg)
}

func init() {
	flags := []cli.Flag{
		&cli.BoolFlag{
			Name:    "agree-tos",
			Usage:   "Agree to the Terms of Service https://fides.ekristen.dev/terms, by using this option you agree.",
			EnvVars: []string{"FIDES_AGREE_TOS"},
		},
		&cli.StringFlag{
			Name:    "namespace",
			Usage:   "namespace for fides",
			Value:   "fides-system",
			EnvVars: []string{"FIDES_NAMESPACE"},
		},
		&cli.StringFlag{
			Name:    "secret-name",
			Usage:   "custom name for the fides secret",
			Value:   "fides",
			EnvVars: []string{"FIDES_SECRET_NAME"},
		},
		&cli.StringFlag{
			Name:  "metrics-port",
			Value: "0.0.0.0:9997",
		},
		&cli.StringFlag{
			Name:   "lockname",
			Value:  "fides-controller",
			Hidden: true,
		},
		&cli.StringFlag{
			Name:    "base-url",
			Value:   "https://fides.ekristen.dev",
			Hidden:  true,
			EnvVars: []string{"FIDES_BASE_URL"},
		},
		&cli.StringFlag{
			Name:    "cluster-key",
			EnvVars: []string{"FIDES_CLUSTER_KEY"},
		},
		&cli.StringFlag{
			Name:    "cluster-name",
			EnvVars: []string{"FIDES_CLUSTER_NAME"},
		},
		&cli.StringFlag{
			Name:    "cluster-id",
			EnvVars: []string{"FIDES_CLUSTER_ID", "FIDES_CLUSTER_UID"},
		},
	}

	cliCmd := &cli.Command{
		Name:    "controller",
		Aliases: []string{"controllers"},
		Usage:   "launches the fides controller",
		Flags:   append(flags, global.Flags()...),
		Before:  global.Before,
		Action:  Execute,
	}

	common.RegisterCommand(cliCmd)
}
