package runonceduration

import (
	"bytes"
	"testing"

	"k8s.io/kubernetes/pkg/admission"
	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	ktestclient "k8s.io/kubernetes/pkg/client/unversioned/testclient"

	oadmission "github.com/openshift/origin/pkg/cmd/server/admission"
	projectcache "github.com/openshift/origin/pkg/project/cache"
	"github.com/openshift/origin/pkg/quota/admission/runonceduration/api"

	_ "github.com/openshift/origin/pkg/api/install"
)

func testCache(projectAnnotations map[string]string) *projectcache.ProjectCache {
	kclient := &ktestclient.Fake{}
	pCache := projectcache.NewFake(kclient.Namespaces(), projectcache.NewCacheStore(cache.MetaNamespaceKeyFunc), "")
	ns := &kapi.Namespace{}
	ns.Name = "default"
	ns.Annotations = projectAnnotations
	pCache.Store.Add(ns)
	return pCache
}

func testConfig(n *int64) *api.RunOnceDurationConfig {
	return &api.RunOnceDurationConfig{
		ActiveDeadlineSecondsOverride: n,
		Enabled: true,
	}
}

func testRunOncePod() *kapi.Pod {
	pod := &kapi.Pod{}
	pod.Spec.RestartPolicy = kapi.RestartPolicyNever
	return pod
}

func testRestartOnFailurePod() *kapi.Pod {
	pod := &kapi.Pod{}
	pod.Spec.RestartPolicy = kapi.RestartPolicyOnFailure
	return pod
}

func testRunOncePodWithDuration(n int64) *kapi.Pod {
	pod := testRunOncePod()
	pod.Spec.ActiveDeadlineSeconds = &n
	return pod
}

func testRestartAlwaysPod() *kapi.Pod {
	pod := &kapi.Pod{}
	pod.Spec.RestartPolicy = kapi.RestartPolicyAlways
	return pod
}

func int64p(n int64) *int64 {
	return &n
}

func TestRunOnceDurationAdmit(t *testing.T) {
	tests := []struct {
		name                          string
		config                        *api.RunOnceDurationConfig
		pod                           *kapi.Pod
		projectAnnotations            map[string]string
		expectedActiveDeadlineSeconds *int64
	}{
		{
			name:   "expect globally configured duration to be set",
			config: testConfig(int64p(10)),
			pod:    testRunOncePod(),
			expectedActiveDeadlineSeconds: int64p(10),
		},
		{
			name:   "empty config, no duration to be set",
			config: testConfig(nil),
			pod:    testRunOncePod(),
			expectedActiveDeadlineSeconds: nil,
		},
		{
			name:   "expect configured duration to override existing duration",
			config: testConfig(int64p(10)),
			pod:    testRunOncePodWithDuration(5),
			expectedActiveDeadlineSeconds: int64p(10),
		},
		{
			name:   "expect empty config to not override existing duration",
			config: testConfig(nil),
			pod:    testRunOncePodWithDuration(5),
			expectedActiveDeadlineSeconds: int64p(5),
		},
		{
			name:   "expect project override to be used with nil global value",
			config: testConfig(nil),
			pod:    testRunOncePodWithDuration(5),
			projectAnnotations: map[string]string{
				api.ActiveDeadlineSecondsOverrideAnnotation: "1000",
			},
			expectedActiveDeadlineSeconds: int64p(1000),
		},
		{
			name:   "expect project override to have priority over global config value",
			config: testConfig(int64p(10)),
			pod:    testRunOncePodWithDuration(5),
			projectAnnotations: map[string]string{
				api.ActiveDeadlineSecondsOverrideAnnotation: "1000",
			},
			expectedActiveDeadlineSeconds: int64p(1000),
		},
		{
			name:   "make no change to a pod that is not a run-once pod",
			config: testConfig(int64p(10)),
			pod:    testRestartAlwaysPod(),
			expectedActiveDeadlineSeconds: nil,
		},
		{
			name:   "update a pod that has a RestartOnFailure policy",
			config: testConfig(int64p(10)),
			pod:    testRestartOnFailurePod(),
			expectedActiveDeadlineSeconds: int64p(10),
		},
	}

	for _, tc := range tests {
		runOnceDuration := NewRunOnceDuration(tc.config)
		runOnceDuration.(oadmission.WantsProjectCache).SetProjectCache(testCache(tc.projectAnnotations))
		pod := tc.pod
		attrs := admission.NewAttributesRecord(pod, kapi.Kind("Pod"), "default", "test", kapi.Resource("pods"), "", admission.Create, nil)
		err := runOnceDuration.Admit(attrs)
		if err != nil {
			t.Errorf("%s: unexpected admission error: %v", tc.name, err)
			continue
		}
		switch {
		case tc.expectedActiveDeadlineSeconds == nil && pod.Spec.ActiveDeadlineSeconds == nil:
			// continue
		case tc.expectedActiveDeadlineSeconds == nil && pod.Spec.ActiveDeadlineSeconds != nil:
			t.Errorf("%s: expected nil ActiveDeadlineSeconds. Got: %d", tc.name, *pod.Spec.ActiveDeadlineSeconds)
		case tc.expectedActiveDeadlineSeconds != nil && pod.Spec.ActiveDeadlineSeconds == nil:
			t.Errorf("%s: unexpected nil ActiveDeadlineSeconds.", tc.name)
		case *pod.Spec.ActiveDeadlineSeconds != *tc.expectedActiveDeadlineSeconds:
			t.Errorf("%s: unexpected active deadline seconds: %d", tc.name, *pod.Spec.ActiveDeadlineSeconds)
		}
	}
}

func TestReadConfig(t *testing.T) {
	configStr := `apiVersion: v1
kind: RunOnceDurationConfig
activeDeadlineSecondsOverride: 3600
enabled: true
`
	buf := bytes.NewBufferString(configStr)
	config, err := readConfig(buf)
	if err != nil {
		t.Fatalf("unexpected error reading config: %v", err)
	}
	if config.ActiveDeadlineSecondsOverride == nil {
		t.Fatalf("nil value for ActiveDeadlineSecondsOverride")
	}
	if *config.ActiveDeadlineSecondsOverride != 3600 {
		t.Errorf("unexpected value for ActiveDeadlineSecondsOverride: %d", config.ActiveDeadlineSecondsOverride)
	}
	if !config.Enabled {
		t.Errorf("unexpected value for Enabled")
	}
}
