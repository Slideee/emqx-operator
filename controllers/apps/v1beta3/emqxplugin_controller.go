/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appsv1beta3 "github.com/emqx/emqx-operator/apis/apps/v1beta3"
	"github.com/emqx/emqx-operator/pkg/handler"
)

type pluginByAPIReturn struct {
	Name        string
	Version     string
	Description string
	Active      bool
	Type        string
}

type pluginListByAPIReturn struct {
	Node    string
	Plugins []pluginByAPIReturn
}

// EmqxPluginReconciler reconciles a EmqxPlugin object
type EmqxPluginReconciler struct {
	handler.Handler
}

//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxplugins/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EmqxPlugin object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *EmqxPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.V(1).Info("Reconcile EmqxPlugin")

	instance := &appsv1beta3.EmqxPlugin{}
	if err := r.Handler.Get(ctx, req.NamespacedName, instance); err != nil {
		if k8sErrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	instance.APIVersion = appsv1beta3.GroupVersion.Group + "/" + appsv1beta3.GroupVersion.Version
	instance.Kind = "EmqxPlugin"

	emqxList, err := r.getEmqxList(instance.Namespace, instance.Spec.Selector)
	if err != nil {
		return ctrl.Result{}, err
	}

	finalizer := "apps.emqx.io/finalizer"
	if instance.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(instance, finalizer) {
			for _, emqx := range emqxList {
				if err := r.unloadPluginByAPI(emqx, instance.Spec.PluginName); err != nil {
					return ctrl.Result{}, err
				}
				if err := r.unloadPluginConfig(instance, emqx); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Remove Finalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(instance, finalizer)
			err := r.Client.Update(ctx, instance)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer for this CR
	if !controllerutil.ContainsFinalizer(instance, finalizer) {
		controllerutil.AddFinalizer(instance, finalizer)
		err := r.Client.Update(ctx, instance)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	for _, emqx := range emqxList {
		ready := false
		for _, c := range emqx.GetStatus().Conditions {
			if c.Type == appsv1beta3.ConditionRunning && c.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			break
		}

		equalPluginConfig, err := r.checkPluginConfig(instance, emqx)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !equalPluginConfig {
			if err := r.loadPluginConfig(instance, emqx); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
		}

		err = r.checkPluginStatusByAPI(emqx, instance.Spec.PluginName)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if instance.Status.Phase != appsv1beta3.EmqxPluginStatusLoaded {
		instance.Status.Phase = appsv1beta3.EmqxPluginStatusLoaded
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Duration(40) * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EmqxPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1beta3.EmqxPlugin{}).
		Complete(r)
}

func (r *EmqxPluginReconciler) getManagementField(obj appsv1beta3.Emqx) (username, password, apiPort string) {
	username = "admin"
	password = "public"
	apiPort = "8081"

	pluginsList := &appsv1beta3.EmqxPluginList{}
	_ = r.Client.List(context.TODO(), pluginsList, client.InNamespace(obj.GetNamespace()))

	for _, plugin := range pluginsList.Items {
		selector, _ := labels.ValidatedSelectorFromSet(plugin.Spec.Selector)
		if selector.Empty() || !selector.Matches(labels.Set(obj.GetLabels())) {
			continue
		}
		if plugin.Spec.PluginName == "emqx_management" {
			if _, ok := plugin.Spec.Config["management.listener.http"]; ok {
				apiPort = plugin.Spec.Config["management.listener.http"]
			}
			if _, ok := plugin.Spec.Config["management.default_application.id"]; ok {
				username = plugin.Spec.Config["management.default_application.id"]
			}
			if _, ok := plugin.Spec.Config["management.default_application.secret"]; ok {
				password = plugin.Spec.Config["management.default_application.secret"]
			}
		}
	}

	return
}

func (r *EmqxPluginReconciler) checkPluginStatusByAPI(emqx appsv1beta3.Emqx, pluginName string) error {
	list, err := r.getPluginsByAPI(emqx)
	if err != nil {
		return err
	}
	for _, node := range list {
		for _, plugin := range node.Plugins {
			if plugin.Name == pluginName {
				if !plugin.Active {
					err := r.doLoadPluginByAPI(emqx, node.Node, plugin.Name, "reload")
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (r *EmqxPluginReconciler) unloadPluginByAPI(emqx appsv1beta3.Emqx, pluginName string) error {
	list, err := r.getPluginsByAPI(emqx)
	if err != nil {
		return err
	}
	for _, node := range list {
		for _, plugin := range node.Plugins {
			if plugin.Name == pluginName {
				err := r.doLoadPluginByAPI(emqx, node.Node, plugin.Name, "unload")
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *EmqxPluginReconciler) doLoadPluginByAPI(emqx appsv1beta3.Emqx, nodeName, pluginName, reloadOrUnload string) error {
	username, password, apiPort := r.getManagementField(emqx)
	resp, _, err := r.Handler.RequestAPI(emqx, "PUT", username, password, apiPort, fmt.Sprintf("api/v4/nodes/%s/plugins/%s/%s", nodeName, pluginName, reloadOrUnload))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("request api failed: %s", resp.Status)
	}
	return nil
}

func (r *EmqxPluginReconciler) getPluginsByAPI(emqx appsv1beta3.Emqx) ([]pluginListByAPIReturn, error) {
	var data []pluginListByAPIReturn

	username, password, apiPort := r.getManagementField(emqx)
	resp, body, err := r.Handler.RequestAPI(emqx, "GET", username, password, apiPort, "api/v4/plugins")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("request api failed: %s", resp.Status)
	}

	err = json.Unmarshal([]byte(gjson.GetBytes(body, "data").String()), &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (r *EmqxPluginReconciler) checkPluginConfig(plugin *appsv1beta3.EmqxPlugin, emqx appsv1beta3.Emqx) (bool, error) {
	pluginConfigStr := generateConfigStr(plugin)

	pluginsConfig, err := r.getPluginsConfig(emqx)
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	configMapStr, err := json.ConfigCompatibleWithStandardLibrary.Marshal(pluginsConfig.Data)
	if err != nil {
		return false, err
	}

	path := plugin.Spec.PluginName + "\\.conf"
	storePluginConfig := gjson.GetBytes(configMapStr, path)
	if storePluginConfig.Exists() {
		if storePluginConfig.String() == pluginConfigStr {
			return true, nil
		}
	}
	return false, nil
}

func (r *EmqxPluginReconciler) loadPluginConfig(plugin *appsv1beta3.EmqxPlugin, emqx appsv1beta3.Emqx) error {
	pluginConfigStr := generateConfigStr(plugin)

	pluginsConfig, err := r.getPluginsConfig(emqx)
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	configMapStr, err := json.ConfigCompatibleWithStandardLibrary.Marshal(pluginsConfig.Data)
	if err != nil {
		return err
	}

	path := plugin.Spec.PluginName + "\\.conf"

	newConfigMapStr, err := sjson.SetBytes(configMapStr, path, pluginConfigStr)
	if err != nil {
		return err
	}

	configData := map[string]string{}
	if err := json.Unmarshal(newConfigMapStr, &configData); err != nil {
		return err
	}
	pluginsConfig.Data = configData

	// Update plugin config
	if err := r.Handler.Update(pluginsConfig, func(_ client.Object) error { return nil }); err != nil {
		return err
	}

	// Update loaded plugins
	loadedPlugins, err := r.getLoadedPlugins(emqx)
	if err != nil {
		return err
	}
	loadedPluginsStr := loadedPlugins.Data["loaded_plugins"]
	index := strings.Index(loadedPluginsStr, plugin.Spec.PluginName)
	if index == -1 {
		loadedPluginLine := plugin.Spec.PluginName + ".\n"
		loadedPluginsStr += loadedPluginLine
		loadedPlugins.Data = map[string]string{"loaded_plugins": loadedPluginsStr}
		if err := r.Handler.Update(loadedPlugins, func(_ client.Object) error { return nil }); err != nil {
			return err
		}
	}
	return nil
}

func (r *EmqxPluginReconciler) unloadPluginConfig(plugin *appsv1beta3.EmqxPlugin, emqx appsv1beta3.Emqx) error {
	// Update loaded plugins
	postfun := func(_ client.Object) error { return nil }
	loadedPlugins, err := r.getLoadedPlugins(emqx)
	if err != nil {
		return err
	}
	loadedPluginsStr := loadedPlugins.Data["loaded_plugins"]
	index := strings.Index(loadedPluginsStr, plugin.Spec.PluginName)
	if index != -1 {
		loadedPluginLine := plugin.Spec.PluginName + ".\n"
		loadedPluginsStr = loadedPluginsStr[:index] + loadedPluginsStr[index+len(loadedPluginLine):]
		loadedPlugins.Data = map[string]string{"loaded_plugins": loadedPluginsStr}
		if err := r.Handler.Update(loadedPlugins, postfun); err != nil {
			return err
		}
	}
	return nil
}

func (r *EmqxPluginReconciler) getEmqxList(namespace string, labels map[string]string) ([]appsv1beta3.Emqx, error) {
	var emqxList []appsv1beta3.Emqx

	emqxBrokerList := &appsv1beta3.EmqxBrokerList{}
	if err := r.List(context.Background(), emqxBrokerList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		if !k8sErrors.IsNotFound(err) {
			return nil, err
		}
	}
	for _, emqxBroker := range emqxBrokerList.Items {
		emqxList = append(emqxList, &emqxBroker)
	}

	emqxEnterpriseList := &appsv1beta3.EmqxEnterpriseList{}
	if err := r.List(context.Background(), emqxEnterpriseList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		if !k8sErrors.IsNotFound(err) {
			return nil, err
		}
	}
	for _, emqxEnterprise := range emqxEnterpriseList.Items {
		emqxList = append(emqxList, &emqxEnterprise)
	}

	return emqxList, nil
}

func (r *EmqxPluginReconciler) getPluginsConfig(emqx appsv1beta3.Emqx) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	if err := r.Get(
		context.TODO(),
		client.ObjectKey{
			Name:      fmt.Sprintf("%s-%s", emqx.GetName(), "plugins-config"),
			Namespace: emqx.GetNamespace(),
		},
		configMap,
	); err != nil {
		return nil, err
	}
	return configMap, nil
}

func (r *EmqxPluginReconciler) getLoadedPlugins(emqx appsv1beta3.Emqx) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	if err := r.Get(
		context.TODO(),
		client.ObjectKey{
			Name:      fmt.Sprintf("%s-%s", emqx.GetName(), "loaded-plugins"),
			Namespace: emqx.GetNamespace(),
		},
		configMap,
	); err != nil {
		return nil, err
	}
	return configMap, nil
}

func generateConfigStr(plugin *appsv1beta3.EmqxPlugin) string {
	keys := make([]string, 0)
	for k := range plugin.Spec.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var config string
	for _, k := range keys {
		config += fmt.Sprintln(k, " = ", plugin.Spec.Config[k])
	}
	return config
}
