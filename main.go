package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"go.alekc.dev/publicip"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const fieldManagerName = "k8s.alekc.dev/kpubber"

const ConfigUseKubeConfig = "USE_CONFIG"
const ConfigKubeConfigPath = "KUBE_CONFIG_PATH"
const ConfigNodeName = "NODE_NAME"
const ConfigAnnotations = "KEYS"
const ConfigCron = "CRON"
const ConfigCronDisable = "CRON_DISABLE"

type Patch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}
type PatchSet []Patch

// JSON returns Json
func (p *PatchSet) JSON() []byte {
	payloadBytes, _ := json.Marshal(p)
	return payloadBytes
}

func main() {
	loadConfig()
	loadLogging()

	publicip.SetMirrors([]string{
		"https://api.ipify.org",
		"http://checkip.amazonaws.com",
	})

	annotationKeys := viper.GetStringSlice(ConfigAnnotations)
	if len(annotationKeys) == 0 {
		log.Fatal().
			Msg("at least one annotation is required")
	}

	k8sClient := loadK8sClient()
	// set up cron if it's not disabled
	if !viper.GetBool(ConfigCronDisable) {
		c := cron.New(cron.WithSeconds())
		_, err := c.AddFunc(viper.GetString(ConfigCron), func() {
			patchNodeAnnotations(k8sClient, annotationKeys)
		})
		if err != nil {
			log.Fatal().Err(err).Msg("cannot set up cronjob")
			return
		}
		c.Start()
	}

	// perform first run without waiting for cron
	patchNodeAnnotations(k8sClient, annotationKeys)
	select {}
}

func patchNodeAnnotations(k8sClient *kubernetes.Clientset, keys []string) {
	nodeName := viper.GetString(ConfigNodeName)
	logger := log.With().
		Str("node_name", nodeName).
		Logger()

	logger.Debug().Msg("preparing to patch")

	ctx := context.Background()
	ip, err := publicip.Get()
	if err != nil {
		logger.
			Err(err).
			Msg("cannot obtain public ip")
		os.Exit(1)
		return
	}
	logger = logger.With().Str("public_ip", ip).Logger()

	patchSet := PatchSet{}
	for _, key := range keys {
		// sanitize key name (see http://jsonpatch.com)
		key = strings.ReplaceAll(key, "~", "~0")
		key = strings.ReplaceAll(key, "/", "~1")

		// append patches to the list
		patchSet = append(patchSet, Patch{
			Op:    "replace",
			Path:  fmt.Sprintf("/metadata/annotations/%s", key),
			Value: ip,
		})
	}
	_, err = k8sClient.CoreV1().Nodes().Patch(ctx, nodeName, types.JSONPatchType, patchSet.JSON(),
		v1.PatchOptions{
			FieldManager: fieldManagerName,
		})
	if err != nil {
		logger.Err(err).
			Str("node_name", nodeName).
			Msg("cannot patch the node annotations")
		return
	}
	logger.Debug().Msg("patched")
}

// it has some curious side effect. For now I leave it disabled for next release
// func setExternalIP(ctx context.Context,node *coreV1.Node, k8sClient *kubernetes.Clientset, ip string) {
// 	newNode := node.DeepCopy()
// 	found := false
// 	for k,v := range newNode.Status.Addresses{
// 		if v.Type != coreV1.NodeExternalIP {
// 			continue
// 		}
// 		// we found it, so let's set it up
// 		newNode.Status.Addresses[k].Address = ip
// 		found = true
// 		break
// 	}
// 	// if we have not found the entry with node external ip, lets set it from here
// 	if !found {
// 		newNode.Status.Addresses = append(newNode.Status.Addresses,coreV1.NodeAddress{
// 			Type:    coreV1.NodeExternalIP,
// 			Address: ip,
// 		})
// 	}
// 	//
// 	// data := nodePatch{Status: nodeStatusPatch{
// 	// 	Addresses: node.Status.Addresses,
// 	// }}
// 	// patchContent, err := json.Marshal(data)
// 	// if err != nil {
// 	// 	log.Err(err).Msg("cannot marshal patch")
// 	// 	return
// 	// }
//
// 	status, err := k8sClient.CoreV1().Nodes().PatchStatus(ctx,newNode.Name,patchContent)
// 	if err != nil {
// 		fmt.Println(status)
// 		return
// 	}
// 	// _, err := k8sClient.CoreV1().Nodes(). UpdateStatus(ctx, newNode,v1.UpdateOptions{
// 	// 	FieldManager: fieldManagerName,
// 	// })
// 	// if err != nil {
// 	// 	log.Err(err).
// 	// 		Msg("cannot update node's status")
// 	// 	return
// 	// }
//
// }

func loadK8sClient() *kubernetes.Clientset {
	var config *rest.Config
	var err error

	switch {
	case viper.GetBool(ConfigUseKubeConfig):
		log.Debug().Msg("using kubeconfig configuration")
		configPath := viper.GetString(ConfigKubeConfigPath)
		config, err = clientcmd.BuildConfigFromFlags("", configPath)
		if err != nil {
			log.Fatal().
				Err(err).
				Str("config_path", configPath).
				Msg("cannot build kubeconfig configuration")
		}
	default:
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("cannot build kubernetes client configuration from cluster role")
		}
	}
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("cannot connect to cluster")
	}
	return clientSet
}

func loadLogging() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	zerolog.TimeFieldFormat = time.RFC3339
}

func loadConfig() {
	viper.AutomaticEnv()
	viper.SetDefault(ConfigUseKubeConfig, false)
	viper.SetDefault(ConfigKubeConfigPath, filepath.Join(os.Getenv("HOME"), ".kube", "config"))
	viper.SetDefault(ConfigCron, "@every 5m")
}
