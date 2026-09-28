/*
Copyright 2022 The Machine Controller Authors.

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
	"testing"

	"github.com/anexia/go-anxsdk/v1/vsphere"
	"github.com/gophercloud/gophercloud/testhelper"

	corev1 "k8s.io/api/core/v1"
)

func TestAnexiaInstance(t *testing.T) {
	addressCheck := func(t *testing.T, testcase string, instance *anexiaInstance, expected map[string]corev1.NodeAddressType) {
		t.Run(testcase, func(t *testing.T) {
			addresses := instance.Addresses()

			testhelper.AssertDeepEquals(t, expected, addresses)
		})
	}

	t.Run("empty instance", func(t *testing.T) {
		instance := anexiaInstance{}
		addressCheck(t, "no addresses", &instance, map[string]corev1.NodeAddressType{})
	})

	t.Run("instance with only reservedAddresses set", func(t *testing.T) {
		instance := anexiaInstance{
			reservedAddresses: []string{"10.0.0.2", "fda0:23::2", "8.8.8.8", "2001:db8::2"},
		}

		addressCheck(t, "expected addresses", &instance, map[string]corev1.NodeAddressType{
			"10.0.0.2":    corev1.NodeInternalIP,
			"fda0:23::2":  corev1.NodeInternalIP,
			"8.8.8.8":     corev1.NodeExternalIP,
			"2001:db8::2": corev1.NodeExternalIP,
		})
	})

	t.Run("instance with only info set", func(t *testing.T) {
		instance := anexiaInstance{
			info: &vsphere.InfoGetResponse{
				Network: []vsphere.InfoGetResponseNetwork{
					{
						IPsv4: []string{"10.0.0.2"},
						IPsv6: []string{"fda0:23::2"},
					},
					{
						IPsv4: []string{"8.8.8.8"},
						IPsv6: []string{"2001:db8::2"},
					},
				},
			},
		}

		addressCheck(t, "expected addresses", &instance, map[string]corev1.NodeAddressType{
			"10.0.0.2":    corev1.NodeInternalIP,
			"fda0:23::2":  corev1.NodeInternalIP,
			"8.8.8.8":     corev1.NodeExternalIP,
			"2001:db8::2": corev1.NodeExternalIP,
		})
	})

	t.Run("instance with both reservedAddresses and info set, full overlapping set", func(t *testing.T) {
		instance := anexiaInstance{
			reservedAddresses: []string{"10.0.0.2", "fda0:23::2", "8.8.8.8", "2001:db8::2"},
			info: &vsphere.InfoGetResponse{
				Network: []vsphere.InfoGetResponseNetwork{
					{
						IPsv4: []string{"10.0.0.2"},
						IPsv6: []string{"fda0:23::2"},
					},
					{
						IPsv4: []string{"8.8.8.8"},
						IPsv6: []string{"2001:db8::2"},
					},
				},
			},
		}

		addressCheck(t, "expected addresses", &instance, map[string]corev1.NodeAddressType{
			"10.0.0.2":    corev1.NodeInternalIP,
			"fda0:23::2":  corev1.NodeInternalIP,
			"8.8.8.8":     corev1.NodeExternalIP,
			"2001:db8::2": corev1.NodeExternalIP,
		})
	})

	t.Run("instance with both reservedAddresses and info set, some overlap, each adding some", func(t *testing.T) {
		instance := anexiaInstance{
			reservedAddresses: []string{"10.0.0.2", "8.8.8.8", "2001:db8::2"},
			info: &vsphere.InfoGetResponse{
				Network: []vsphere.InfoGetResponseNetwork{
					{
						IPsv4: []string{"10.0.0.2"},
						IPsv6: []string{"fda0:23::2"},
					},
					{
						IPsv6: []string{"2001:db8::2"},
					},
				},
			},
		}

		addressCheck(t, "expected addresses", &instance, map[string]corev1.NodeAddressType{
			"10.0.0.2":    corev1.NodeInternalIP,
			"fda0:23::2":  corev1.NodeInternalIP,
			"8.8.8.8":     corev1.NodeExternalIP,
			"2001:db8::2": corev1.NodeExternalIP,
		})
	})
}
