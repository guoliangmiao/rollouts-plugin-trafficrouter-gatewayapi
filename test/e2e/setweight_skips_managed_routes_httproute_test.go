//go:build flaky

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TestSetWeightSkipsManagedRoutesHTTPRoute verifies that when setWeight is called,
// the weights of managed routes (header routes created by setHeaderRoute) are NOT modified.
// The test flow is:
// 1. Create HTTPRoute and Rollout
// 2. Trigger canary deployment (image change) which calls setHeaderRoute first, then setWeight
// 3. Verify that the header route weight remains nil (100% to canary) while the main route weight is updated
func TestSetWeightSkipsManagedRoutesHTTPRoute(t *testing.T) {
	feature := features.New("SetWeight skips managed routes for HTTPRoute").Setup(
		setupEnvironment,
	).Setup(
		setupSetWeightSkipsManagedRoutesHTTPRouteEnv,
	).Assess(
		"Verifying setWeight does not modify managed route weights",
		testSetWeightSkipsManagedRoutesHTTPRoute,
	).Teardown(
		teardownSetWeightSkipsManagedRoutesHTTPRouteEnv,
	).Feature()
	_ = global.Test(t, feature)
}

func setupSetWeightSkipsManagedRoutesHTTPRouteEnv(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
	var httpRoute gatewayv1.HTTPRoute
	var rollout v1alpha1.Rollout
	clusterResources := config.Client().Resources()
	resourcesMap := map[string]*unstructured.Unstructured{}
	ctx = context.WithValue(ctx, RESOURCES_MAP_KEY, resourcesMap)
	firstHTTPRouteFile, err := os.Open(HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_PATH)
	if err != nil {
		logrus.Errorf("file %q openning was failed: %s", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_PATH, err)
		t.Error()
		return ctx
	}
	defer firstHTTPRouteFile.Close()
	logrus.Infof("file %q was opened", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_PATH)
	rolloutFile, err := os.Open(HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_ROLLOUT_PATH)
	if err != nil {
		logrus.Errorf("file %q openning was failed: %s", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_ROLLOUT_PATH, err)
		t.Error()
		return ctx
	}
	defer rolloutFile.Close()
	logrus.Infof("file %q was opened", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_ROLLOUT_PATH)
	err = decoder.Decode(firstHTTPRouteFile, &httpRoute)
	if err != nil {
		logrus.Errorf("file %q decoding was failed: %s", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_PATH, err)
		t.Error()
		return ctx
	}
	logrus.Infof("file %q was decoded", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_PATH)
	err = decoder.Decode(rolloutFile, &rollout)
	if err != nil {
		logrus.Errorf("file %q decoding was failed: %s", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_ROLLOUT_PATH, err)
		t.Error()
		return ctx
	}
	logrus.Infof("file %q was decoded", HTTP_ROUTE_SETWEIGHT_SKIPS_MANAGED_ROLLOUT_PATH)
	httpRouteObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&httpRoute)
	if err != nil {
		logrus.Errorf("httpRoute %q converting to unstructured was failed: %s", httpRoute.GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("httpRoute %q was converted to unstructured", httpRoute.GetName())
	resourcesMap[HTTP_ROUTE_KEY] = &unstructured.Unstructured{
		Object: httpRouteObject,
	}
	rolloutObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&rollout)
	if err != nil {
		logrus.Errorf("rollout %q converting to unstructured was failed: %s", rollout.GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q was converted to unstructured", rollout.GetName())
	unstructured.RemoveNestedField(rolloutObject, "spec", "template", "metadata", "creationTimestamp")
	resourcesMap[ROLLOUT_KEY] = &unstructured.Unstructured{
		Object: rolloutObject,
	}
	err = clusterResources.Create(ctx, resourcesMap[HTTP_ROUTE_KEY])
	if err != nil {
		logrus.Errorf("httpRoute %q creation was failed: %s", resourcesMap[HTTP_ROUTE_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("httpRoute %q was created", resourcesMap[HTTP_ROUTE_KEY].GetName())
	err = clusterResources.Create(ctx, resourcesMap[ROLLOUT_KEY])
	if err != nil {
		logrus.Errorf("rollout %q creation was failed: %s", resourcesMap[ROLLOUT_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q was created", resourcesMap[ROLLOUT_KEY].GetName())
	waitCondition := conditions.New(clusterResources)
	logrus.Infof("waiting for httpRoute %q to connect with rollout %q (expecting canary weight: %d)", resourcesMap[HTTP_ROUTE_KEY].GetName(), resourcesMap[ROLLOUT_KEY].GetName(), FIRST_CANARY_ROUTE_WEIGHT)
	err = wait.For(
		waitCondition.ResourceMatch(
			resourcesMap[HTTP_ROUTE_KEY],
			getMatchHTTPRouteFetcher(t, FIRST_CANARY_ROUTE_WEIGHT),
		),
		wait.WithTimeout(MEDIUM_PERIOD),
		wait.WithInterval(SHORT_PERIOD),
	)
	if err != nil {
		logrus.Errorf("checking httpRoute %q connection with rollout %q was failed: %s", resourcesMap[HTTP_ROUTE_KEY].GetName(), resourcesMap[ROLLOUT_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("httpRoute %q connected with rollout %q", resourcesMap[HTTP_ROUTE_KEY].GetName(), resourcesMap[ROLLOUT_KEY].GetName())
	return ctx
}

func testSetWeightSkipsManagedRoutesHTTPRoute(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
	clusterResources := config.Client().Resources()
	resourcesMap, ok := ctx.Value(RESOURCES_MAP_KEY).(map[string]*unstructured.Unstructured)
	if !ok {
		logrus.Errorf("%q type assertion was failed", RESOURCES_MAP_KEY)
		t.Error()
		return ctx
	}
	logrus.Infof("%q was type asserted", RESOURCES_MAP_KEY)
	containersObject, isFound, err := unstructured.NestedFieldNoCopy(resourcesMap[ROLLOUT_KEY].Object, strings.Split(ROLLOUT_TEMPLATE_CONTAINERS_FIELD, ".")...)
	if !isFound {
		logrus.Errorf("rollout %q field %q was not found", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_CONTAINERS_FIELD)
		t.Error()
		return ctx
	}
	if err != nil {
		logrus.Errorf("getting rollout %q field %q was failed: %s", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_CONTAINERS_FIELD, err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q field %q was received", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_CONTAINERS_FIELD)
	unstructuredContainerList, ok := containersObject.([]interface{})
	if !ok {
		logrus.Errorf("rollout %q field %q type assertion was failed", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_CONTAINERS_FIELD)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q field %q was type asserted", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_CONTAINERS_FIELD)
	unstructuredContainer, ok := unstructuredContainerList[0].(map[string]interface{})
	if !ok {
		logrus.Errorf("rollout %q field %q type assertion was failed", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_FIRST_CONTAINER_FIELD)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q field %q was type asserted", resourcesMap[ROLLOUT_KEY].GetName(), ROLLOUT_TEMPLATE_FIRST_CONTAINER_FIELD)
	unstructured.RemoveNestedField(resourcesMap[ROLLOUT_KEY].Object, "metadata", "resourceVersion")
	unstructuredContainer["image"] = NEW_IMAGE_FIELD_VALUE
	serializedRollout, err := json.Marshal(resourcesMap[ROLLOUT_KEY].Object)
	if err != nil {
		logrus.Errorf("rollout %q serializing was failed: %s", resourcesMap[ROLLOUT_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q was serialized", resourcesMap[ROLLOUT_KEY].GetName())
	rolloutPatch := k8s.Patch{
		PatchType: types.MergePatchType,
		Data:      serializedRollout,
	}
	err = clusterResources.Patch(ctx, resourcesMap[ROLLOUT_KEY], rolloutPatch)
	if err != nil {
		logrus.Errorf("rollout %q updating was failed: %s", resourcesMap[ROLLOUT_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q was updated", resourcesMap[ROLLOUT_KEY].GetName())
	waitCondition := conditions.New(clusterResources)
	// Test the key behavior: header route weight should be nil (100% to canary),
	// while main route canary weight should be LAST_CANARY_ROUTE_WEIGHT
	// This verifies that setWeight does NOT modify managed route weights
	logrus.Infof("waiting for httpRoute %q to update with header-based routing where header route weight is nil and main route canary weight is %d", resourcesMap[HTTP_ROUTE_KEY].GetName(), LAST_CANARY_ROUTE_WEIGHT)
	err = wait.For(
		waitCondition.ResourceMatch(
			resourcesMap[HTTP_ROUTE_KEY],
			getMatchManagedRouteWeightNilHTTPRouteFetcher(
				t,
				LAST_CANARY_ROUTE_WEIGHT,
				LAST_HEADER_BASED_HTTP_ROUTE_VALUE,
			),
		),
		wait.WithTimeout(LONG_PERIOD),
		wait.WithInterval(SHORT_PERIOD),
	)
	if err != nil {
		logrus.Errorf("httpRoute %q updating failed: %s", resourcesMap[HTTP_ROUTE_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("httpRoute %q was updated with nil weight for managed header route (setWeight skipped it correctly)", resourcesMap[HTTP_ROUTE_KEY].GetName())
	return ctx
}

func teardownSetWeightSkipsManagedRoutesHTTPRouteEnv(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
	clusterResources := config.Client().Resources()
	resourcesMap, ok := ctx.Value(RESOURCES_MAP_KEY).(map[string]*unstructured.Unstructured)
	if !ok {
		logrus.Errorf("%q type assertion was failed", RESOURCES_MAP_KEY)
		t.Error()
		return ctx
	}
	logrus.Infof("%q was type asserted", RESOURCES_MAP_KEY)
	err := clusterResources.Delete(ctx, resourcesMap[ROLLOUT_KEY])
	if err != nil {
		logrus.Errorf("deleting rollout %q was failed: %s", resourcesMap[ROLLOUT_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("rollout %q was deleted", resourcesMap[ROLLOUT_KEY].GetName())
	err = clusterResources.Delete(ctx, resourcesMap[HTTP_ROUTE_KEY])
	if err != nil {
		logrus.Errorf("deleting httpRoute %q was failed: %s", resourcesMap[HTTP_ROUTE_KEY].GetName(), err)
		t.Error()
		return ctx
	}
	logrus.Infof("httpRoute %q was deleted", resourcesMap[HTTP_ROUTE_KEY].GetName())
	return ctx
}

// getMatchManagedRouteWeightNilHTTPRouteFetcher returns a fetcher that checks:
// 1. The main route's canary weight matches targetWeight
// 2. The header route's weight is nil (meaning 100% traffic to canary for matching requests)
// This verifies the key behavior: setWeight should skip managed routes
func getMatchManagedRouteWeightNilHTTPRouteFetcher(t *testing.T, targetWeight int32, targetHeaderBasedRouteValue gatewayv1.HTTPHeaderMatch) func(k8s.Object) bool {
	return func(obj k8s.Object) bool {
		var httpRoute gatewayv1.HTTPRoute
		unstructuredHTTPRoute, ok := obj.(*unstructured.Unstructured)
		if !ok {
			logrus.Error("k8s object type assertion was failed")
			t.Error()
			return false
		}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredHTTPRoute.Object, &httpRoute)
		if err != nil {
			logrus.Errorf("conversation from unstructured httpRoute %q to the typed httpRoute was failed", unstructuredHTTPRoute.GetName())
			t.Error()
			return false
		}
		rules := httpRoute.Spec.Rules
		// Check that we have 2 rules (main route + header route)
		if len(rules) != FIRST_HEADER_BASED_RULES_LENGTH {
			return false
		}
		// Check main route canary weight
		if *rules[ROLLOUT_ROUTE_RULE_INDEX].BackendRefs[CANARY_BACKEND_REF_INDEX].Weight != targetWeight {
			return false
		}
		// Check header route value matches
		headerBasedRouteValue := rules[HEADER_BASED_RULE_INDEX].Matches[HEADER_BASED_MATCH_INDEX].Headers[HEADER_BASED_HEADER_INDEX]
		// Inline comparison to avoid dependency on flaky-tagged test files
		if headerBasedRouteValue.Name != targetHeaderBasedRouteValue.Name ||
			*headerBasedRouteValue.Type != *targetHeaderBasedRouteValue.Type ||
			headerBasedRouteValue.Value != targetHeaderBasedRouteValue.Value {
			return false
		}
		// KEY CHECK: Verify that header route weight is nil (100% to canary for matching requests)
		// This confirms that setWeight did NOT modify the managed route weight
		weight := rules[HEADER_BASED_RULE_INDEX].BackendRefs[HEADER_BASED_BACKEND_REF_INDEX].Weight
		return weight == nil
	}
}
