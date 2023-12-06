package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ekristen/fides/pkg/common"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/rancher/wrangler/v2/pkg/kubeconfig"
	"github.com/rancher/wrangler/v2/pkg/leader"
	"github.com/sirupsen/logrus"

	"github.com/ekristen/fides/pkg/types"
)

var UserAgent = fmt.Sprintf("fides/%s", common.AppVersion.Summary)

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
	ticker := time.NewTicker(15 * time.Minute)
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

	// 1. check the secret for cluster-id/cluster-key
	// 2. if it does not exist, register the cluster
	// 3. else update the cluster
	newCluster := false
	secret, err := kube.CoreV1().Secrets(config.Namespace).Get(ctx, config.SecretName, v1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	clusterKey := string(secret.Data["cluster-key"])
	clusterID := string(secret.Data["cluster-id"])
	clusterName := string(secret.Data["cluster-name"])

	if clusterKey == "quickstart" {
		newCluster = true
	}

	logrus.Infof("is cluster new: %t", newCluster)

	if !newCluster {
		if config.ClusterKey == "" || config.ClusterKey != clusterKey {
			config.ClusterKey = clusterKey
		}
		if config.ClusterID == "" || config.ClusterID != clusterID {
			config.ClusterID = clusterID
		}
		if config.ClusterName == "" || config.ClusterName != clusterName {
			config.ClusterName = clusterName
		}
	}

	logrus.WithFields(logrus.Fields{
		"cluster-id":   config.ClusterID,
		"cluster-name": config.ClusterName,
	}).Info("cluster information")

	resConfig := kube.RESTClient().Get().AbsPath("/.well-known/openid-configuration").Do(ctx)
	configData, err := resConfig.Raw()
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve openid configuration")
		return err
	}

	resJWKs := kube.RESTClient().Get().AbsPath("/openid/v1/jwks").Do(ctx)
	jwkData, err := resJWKs.Raw()
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve jwks")
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

	if newCluster {
		// register the cluster
		return registerCluster(ctx, kube, config, uid, clusterName, wellKnown, jwks)
	}

	// update the cluster
	return updateCluster(ctx, kube, config, uid, wellKnown, jwks)
}

func updateCluster(ctx context.Context, kube *kubernetes.Clientset, config Config, uid apitypes.UID, wellKnown types.OpenIDConfiguration, jwks types.JWKS) error {
	logrus.Info("updating cluster")

	ctx, cancel := context.WithDeadlineCause(ctx, time.Now().Add(30*time.Second), fmt.Errorf("register cluster"))
	defer cancel()

	reg := types.ClusterPutRequest{
		UID:       string(uid),
		OIDConfig: wellKnown,
		JWKS:      jwks,
	}

	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(reg); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/api/v1/clusters/%s", config.BaseURL, config.ClusterID), b)
	if err != nil {
		return err
	}

	req.Header.Add("User-Agent", UserAgent)

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

	switch res.StatusCode {
	case 200:
		logrus.Info("cluster updated successfully")
	default:
		data, err := io.ReadAll(res.Body)
		if err != nil {
			logrus.WithError(err).Error("unable to read body")
			return err
		}

		logrus.WithField("data", string(data)).WithField("status", res.StatusCode).Error("unknown status code")
	}

	return nil
}

func registerCluster(ctx context.Context, kube *kubernetes.Clientset, config Config, uid apitypes.UID, clusterName string, wellKnown types.OpenIDConfiguration, jwks types.JWKS) error {
	logrus.Info("registering cluster")

	ctx, cancel := context.WithDeadlineCause(ctx, time.Now().Add(30*time.Second), fmt.Errorf("register cluster"))
	defer cancel()

	regInput := types.ClusterNewRequest{
		Name:      clusterName,
		UID:       string(uid),
		OIDConfig: wellKnown,
		JWKS:      jwks,
	}
	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(regInput); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/clusters", config.BaseURL), b)
	if err != nil {
		return err
	}

	req.Header.Add("User-Agent", UserAgent)
	req.Header.Add("x-fides-quickstart", "true")

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

	switch res.StatusCode {
	case 201:
		logrus.Info("cluster registered successfully")
		d, err := io.ReadAll(res.Body)
		if err != nil {
			logrus.WithError(err).Error("unable to read body")
			return err
		}
		var resp types.ClusterNewResponse
		if err := json.Unmarshal(d, &resp); err != nil {
			logrus.WithError(err).Error("unable to parse response")
			return err
		}

		secret, err := kube.CoreV1().Secrets(config.Namespace).Update(ctx, &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      config.SecretName,
				Namespace: config.Namespace,
			},
			StringData: map[string]string{
				"cluster-id":   resp.UID,
				"cluster-key":  resp.Token,
				"cluster-name": resp.Name,
			},
		}, v1.UpdateOptions{})
		if err != nil {
			return err
		}
		_ = secret
	case 409:
		logrus.Error("cluster already exists")
		return fmt.Errorf("cluster already exists")
	default:
		data, err := io.ReadAll(res.Body)
		if err != nil {
			logrus.WithError(err).Error("unable to read body")
			return err
		}
		logrus.WithField("data", string(data)).WithField("status", res.StatusCode).Error("unknown status code")
	}

	return nil
}
