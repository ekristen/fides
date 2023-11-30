package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/rancher/wrangler/pkg/leader"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"time"

	"github.com/ekristen/fides/pkg/types"
)

type Config struct {
	KubeConfigPath string
	Namespace      string
	Lockname       string
	BaseURL        string
	SecretName     string
	ClusterKey     string
	ClusterName    string
	ClusterID      string
}

func NewController(ctx context.Context, config Config) error {
	cfg, err := kubeconfig.GetNonInteractiveClientConfig(config.KubeConfigPath).ClientConfig()
	if err != nil {
		return err
	}

	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	leader.RunOrDie(ctx, config.Namespace, config.Lockname, kube, func(ctx context.Context) {
		logrus.Info("started sync daemon")

		go func() {
			err := sync(ctx, kube, config)
			if err != nil {
				logrus.WithError(err).Fatal("unable to sync")
			}
		}()

		<-ctx.Done()
	})

	return nil
}

func sync(ctx context.Context, kube *kubernetes.Clientset, config Config) error {
	logrus.Info("sync called")

	ks, err := kube.CoreV1().Namespaces().Get(ctx, "kube-system", v1.GetOptions{})
	if err != nil {
		return err
	}

	firstTicker := time.NewTicker(1 * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-firstTicker.C:
			if err := doSync(ctx, kube, config, ks.GetUID()); err != nil {
				logrus.WithError(err).Error("unable to perform sync")
			}
			firstTicker.Stop()
		case <-ticker.C:
			if err := doSync(ctx, kube, config, ks.GetUID()); err != nil {
				logrus.WithError(err).Error("unable to perform sync")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func doSync(ctx context.Context, kube *kubernetes.Clientset, config Config, uid apitypes.UID) error {
	logrus.Info("running doSync")

	resConfig := kube.RESTClient().Get().AbsPath("/.well-known/openid-configuration").Do(ctx)
	configData, err := resConfig.Raw()
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve raw data")
		return err
	}

	resJWKs := kube.RESTClient().Get().AbsPath("/openid/v1/jwks").Do(ctx)
	jwkData, err := resJWKs.Raw()
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve raw data")
		return err
	}

	var wellKnown types.OpenIDConfiguration
	if err := json.Unmarshal(configData, &wellKnown); err != nil {
		return err
	}

	var jwks types.JWKS
	if err := json.Unmarshal(jwkData, &jwks); err != nil {
		return err
	}

	reg := types.ClusterPutRequest{
		UID:       string(uid),
		OIDConfig: wellKnown,
		JWKS:      jwks,
	}

	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(reg); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/clusters/%s", config.BaseURL, config.ClusterID), b)
	if err != nil {
		return err
	}

	if config.ClusterKey != "" {
		// existing cluster token to http request
		req.Header.Add("x-cluster-key", config.ClusterKey)
	}

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logrus.WithError(err).Error("unable to close body")
		}
	}(res.Body)

	if res.StatusCode == 200 {
		logrus.Info("cluster updated successfully")
	} else {
		data, err := ioutil.ReadAll(res.Body)
		if err != nil {
			logrus.WithError(err).Error("unable to read body")
			return err
		}

		var resp types.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			logrus.WithError(err).Error("unable to parse response")
			return err
		}

		logrus.WithError(fmt.Errorf(resp.Error)).Error("an error occurred updating the cluster information")
	}

	return nil
}
