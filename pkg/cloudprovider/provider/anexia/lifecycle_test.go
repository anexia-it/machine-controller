/*
Copyright 2026 The Machine Controller Authors.

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

package anexia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anexia/go-anxsdk"
	"github.com/anexia/go-anxsdk/v1/vsphere"
	"go.uber.org/zap"

	cloudprovidertypes "k8c.io/machine-controller/pkg/cloudprovider/types"
	clusterv1alpha1 "k8c.io/machine-controller/sdk/apis/cluster/v1alpha1"
	anxtypes "k8c.io/machine-controller/sdk/cloudprovider/anexia"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// newTestProvider wires the provider up against the given mux. These tests are
// deliberately not parallel: they swap the package-level getSDKClient seam.
func newTestProvider(t *testing.T, mux *http.ServeMux) (*provider, *clusterv1alpha1.Machine, *cloudprovidertypes.ProviderData) {
	t.Helper()

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	original := getSDKClient
	getSDKClient = func(*string) *anxsdk.Client {
		return anxsdk.NewClient(anxsdk.WithBaseURL(server.URL), anxsdk.WithHTTPClient(server.Client()))
	}
	t.Cleanup(func() { getSDKClient = original })

	machine := &clusterv1alpha1.Machine{ObjectMeta: metav1.ObjectMeta{Name: testMachineName}}
	data := &cloudprovidertypes.ProviderData{
		Update: func(*clusterv1alpha1.Machine, ...cloudprovidertypes.MachineModifier) error { return nil },
	}

	return &provider{}, machine, data
}

func setProviderStatus(t *testing.T, machine *clusterv1alpha1.Machine, status anxtypes.ProviderStatus) {
	t.Helper()

	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshalling provider status: %v", err)
	}
	machine.Status.ProviderStatus = &runtime.RawExtension{Raw: raw}
}

func progressHandler(t *testing.T, taskID string, progress vsphere.ProvisioningProgress) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vsphere/v1/provisioning/progress.json/"+taskID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(progress); err != nil {
			t.Errorf("encoding progress: %v", err)
		}
	})

	return mux
}

// Covers the former panic in isTaskDone: an unexpected status value from the
// API must surface as an error rather than crash the controller.
func TestIsTaskDoneUnknownStatus(t *testing.T) {
	mux := progressHandler(t, testTaskID, vsphere.ProvisioningProgress{
		TaskIdentifier: testTaskID,
		Status:         "something-new",
	})

	p, _, _ := newTestProvider(t, mux)
	_ = p

	client := getSDKClient(nil).V1().VSphere().Provisioning()

	done, err := isTaskDone(context.Background(), client, testTaskID)
	if err == nil {
		t.Fatal("expected an error for an unknown provisioning status, got nil")
	}
	if done {
		t.Error("expected done=false for an unknown provisioning status")
	}
	if !strings.Contains(err.Error(), "something-new") {
		t.Errorf("error should name the unexpected status, got %q", err.Error())
	}
}

func TestIsTaskDoneKnownStatuses(t *testing.T) {
	testCases := []struct {
		name       string
		status     vsphere.ProvisioningStatus
		expectDone bool
		expectErr  bool
	}{
		{"success", vsphere.ProvisioningStatusSuccess, true, false},
		{"in progress", vsphere.ProvisioningStatusInProgress, false, false},
		{"failed", vsphere.ProvisioningStatusFailed, true, true},
		{"cancelled", vsphere.ProvisioningStatusCancelled, true, true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mux := progressHandler(t, testTaskID, vsphere.ProvisioningProgress{
				TaskIdentifier: testTaskID,
				Status:         testCase.status,
			})
			p, _, _ := newTestProvider(t, mux)
			_ = p

			done, err := isTaskDone(context.Background(), getSDKClient(nil).V1().VSphere().Provisioning(), testTaskID)
			if gotErr := err != nil; gotErr != testCase.expectErr {
				t.Errorf("expected error=%v, got %v (%v)", testCase.expectErr, gotErr, err)
			}
			if done != testCase.expectDone {
				t.Errorf("expected done=%v, got %v", testCase.expectDone, done)
			}
		})
	}
}

// Covers the error-shadowing bug: the API failure must reach the caller instead
// of being replaced by the (successful) status update's nil error.
func TestProvisionVMReportsProvisioningError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ipam/v1/address/reserve/ip/count.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"identifier": testIPIdentifier, "text": testPublicIPv4}},
		}); err != nil {
			t.Errorf("encoding address: %v", err)
		}
	})
	mux.HandleFunc("/api/vsphere/v1/provisioning/vm.json/LOCATION-ID/templates/TEMPLATE-ID", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"error": {"message": "engine exploded"}}`)); err != nil {
			t.Errorf("writing error body: %v", err)
		}
	})

	p, _, _ := newTestProvider(t, mux)
	_ = p

	sdkClient := getSDKClient(nil)
	reconcileCtx := hookableReconcileContext("LOCATION-ID", "TEMPLATE-ID", nil)

	err := provisionVM(context.Background(), reconcileCtx, zap.NewNop().Sugar(),
		sdkClient.V1().VSphere().Provisioning(), sdkClient.V1().Ipam().Addresses())
	if err == nil {
		t.Fatal("expected an error when provisioning fails")
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Errorf("provisioning error was lost, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should carry the API failure, got %q", err.Error())
	}
}

// Covers the stuck-cleanup bug: a VM the API no longer knows about means the
// Machine is deleted, so Cleanup must report done instead of polling an empty
// deprovisioning task forever.
func TestCleanupVMAlreadyGone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vsphere/v1/info.json/INSTANCE-ID/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vsphere.InfoGetResponse{
			Identifier: testInstanceID,
			Status:     vsphere.PowerStatePoweredOn,
		}); err != nil {
			t.Errorf("encoding info: %v", err)
		}
	})
	mux.HandleFunc("/api/vsphere/v1/provisioning/vm.json/INSTANCE-ID", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
	})

	p, machine, data := newTestProvider(t, mux)
	setProviderStatus(t, machine, anxtypes.ProviderStatus{InstanceID: testInstanceID})

	deleted, err := p.Cleanup(context.Background(), zap.NewNop().Sugar(), machine, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("expected Cleanup to report the machine as deleted when the VM is gone")
	}
}

// A Machine that never got as far as an instance has nothing to deprovision.
func TestCleanupWithoutInstanceID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vsphere/v1/provisioning/progress.json/PROVISIONING-ID", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(vsphere.ProvisioningProgress{
			TaskIdentifier: testProvisioningID,
			Status:         vsphere.ProvisioningStatusFailed,
			Errors:         []string{"no capacity"},
		}); err != nil {
			t.Errorf("encoding progress: %v", err)
		}
	})

	p, machine, data := newTestProvider(t, mux)
	setProviderStatus(t, machine, anxtypes.ProviderStatus{ProvisioningID: testProvisioningID})

	// Get reports the failed provisioning, which Cleanup surfaces.
	_, err := p.Cleanup(context.Background(), zap.NewNop().Sugar(), machine, data)
	if err == nil {
		t.Fatal("expected the failed provisioning to be reported")
	}
	if !strings.Contains(err.Error(), "no capacity") {
		t.Errorf("error should carry the provisioning failure, got %q", err.Error())
	}
}

// Covers the unhandled Cancelled status in Get, which previously fell through
// to an info lookup with an empty instance identifier.
func TestGetCancelledProvisioning(t *testing.T) {
	mux := progressHandler(t, testProvisioningID, vsphere.ProvisioningProgress{
		TaskIdentifier: testProvisioningID,
		Status:         vsphere.ProvisioningStatusCancelled,
		Errors:         []string{"cancelled by operator"},
	})

	p, machine, data := newTestProvider(t, mux)
	setProviderStatus(t, machine, anxtypes.ProviderStatus{ProvisioningID: testProvisioningID})

	_, err := p.Get(context.Background(), zap.NewNop().Sugar(), machine, data)
	if err == nil {
		t.Fatal("expected an error for a cancelled provisioning task")
	}
	if !strings.Contains(err.Error(), "cancelled by operator") {
		t.Errorf("error should carry the API's reason, got %q", err.Error())
	}
}

func TestGetUnknownProvisioningStatus(t *testing.T) {
	mux := progressHandler(t, testProvisioningID, vsphere.ProvisioningProgress{
		TaskIdentifier: testProvisioningID,
		Status:         "",
	})

	p, machine, data := newTestProvider(t, mux)
	setProviderStatus(t, machine, anxtypes.ProviderStatus{ProvisioningID: testProvisioningID})

	_, err := p.Get(context.Background(), zap.NewNop().Sugar(), machine, data)
	if err == nil {
		t.Fatal("expected an error for an unknown provisioning status")
	}
}

// Get is what promotes the Provisioned condition to True - provisionVM only
// records that the request was accepted.
func TestGetMarksProvisionedOnSuccess(t *testing.T) {
	mux := progressHandler(t, testProvisioningID, vsphere.ProvisioningProgress{
		TaskIdentifier: testProvisioningID,
		Status:         vsphere.ProvisioningStatusSuccess,
		VMIdentifier:   testInstanceID,
	})
	mux.HandleFunc("/api/vsphere/v1/info.json/INSTANCE-ID/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vsphere.InfoGetResponse{
			Identifier: testInstanceID,
			Name:       testMachineName,
			Status:     vsphere.PowerStatePoweredOn,
		}); err != nil {
			t.Errorf("encoding info: %v", err)
		}
	})

	p, machine, data := newTestProvider(t, mux)
	setProviderStatus(t, machine, anxtypes.ProviderStatus{ProvisioningID: testProvisioningID})

	var updated anxtypes.ProviderStatus
	data.Update = func(m *clusterv1alpha1.Machine, mods ...cloudprovidertypes.MachineModifier) error {
		for _, mod := range mods {
			mod(m)
		}
		return json.Unmarshal(m.Status.ProviderStatus.Raw, &updated)
	}

	inst, err := p.Get(context.Background(), zap.NewNop().Sugar(), machine, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.ID() != testInstanceID {
		t.Errorf("expected instance ID %q, got %q", testInstanceID, inst.ID())
	}
	if updated.InstanceID != testInstanceID {
		t.Errorf("expected the instance ID to be persisted, got %q", updated.InstanceID)
	}

	for _, condition := range updated.Conditions {
		if condition.Type == ProvisionedType {
			if condition.Status != metav1.ConditionTrue {
				t.Errorf("expected %s=True, got %s", ProvisionedType, condition.Status)
			}
			return
		}
	}
	t.Errorf("expected a %s condition to be set", ProvisionedType)
}

// provisionVM must not claim the machine is provisioned before the task ran.
func TestProvisionVMDoesNotClaimProvisioned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ipam/v1/address/reserve/ip/count.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"identifier": testIPIdentifier, "text": testPublicIPv4}},
		}); err != nil {
			t.Errorf("encoding address: %v", err)
		}
	})
	mux.HandleFunc("/api/vsphere/v1/provisioning/vm.json/LOCATION-ID/TEMPLATE-ID", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/api/vsphere/v1/provisioning/vm.json/LOCATION-ID/templates/TEMPLATE-ID", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vsphere.ProvisioningResponse{TaskIdentifier: testTaskID}); err != nil {
			t.Errorf("encoding provisioning response: %v", err)
		}
	})

	p, _, _ := newTestProvider(t, mux)
	_ = p

	sdkClient := getSDKClient(nil)
	reconcileCtx := hookableReconcileContext("LOCATION-ID", "TEMPLATE-ID", nil)

	if err := provisionVM(context.Background(), reconcileCtx, zap.NewNop().Sugar(),
		sdkClient.V1().VSphere().Provisioning(), sdkClient.V1().Ipam().Addresses()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, condition := range reconcileCtx.Status.Conditions {
		if condition.Type == ProvisionedType && condition.Status == metav1.ConditionTrue {
			t.Error("provisionVM must not report Provisioned=True before the task completed")
		}
	}
}
