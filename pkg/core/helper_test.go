package core

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "test-ns"

var testLabels = map[string]string{"name": "dbaas-adapter"}

func testPod(name string, phase v1.PodPhase, ready bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: testLabels},
		Status: v1.PodStatus{
			Phase:             phase,
			ContainerStatuses: []v1.ContainerStatus{{Name: "main", Ready: ready}},
		},
	}
}

func terminatingPod(name string) *v1.Pod {
	pod := testPod(name, v1.PodRunning, true)
	now := metav1.NewTime(time.Now())
	pod.DeletionTimestamp = &now
	pod.Finalizers = []string{"test/finalizer"}
	return pod
}

func TestCheckPodsReadyByLabel(t *testing.T) {
	tests := []struct {
		name         string
		pods         []client.Object
		numberOfPods int
		want         bool
	}{
		{
			name:         "all pods ready",
			pods:         []client.Object{testPod("a", v1.PodRunning, true)},
			numberOfPods: 1,
			want:         true,
		},
		{
			name:         "pod not ready",
			pods:         []client.Object{testPod("a", v1.PodRunning, false)},
			numberOfPods: 1,
			want:         false,
		},
		{
			name:         "pod pending",
			pods:         []client.Object{testPod("a", v1.PodPending, false)},
			numberOfPods: 1,
			want:         false,
		},
		{
			name: "evicted pod next to its ready replacement",
			pods: []client.Object{
				testPod("evicted", v1.PodFailed, false),
				testPod("a", v1.PodRunning, true),
			},
			numberOfPods: 1,
			want:         true,
		},
		{
			name: "terminating pod next to its ready replacement",
			pods: []client.Object{
				terminatingPod("old"),
				testPod("a", v1.PodRunning, true),
			},
			numberOfPods: 1,
			want:         true,
		},
		{
			name:         "only evicted pod left",
			pods:         []client.Object{testPod("evicted", v1.PodFailed, false)},
			numberOfPods: 1,
			want:         false,
		},
		{
			name:         "more ready pods than expected",
			pods:         []client.Object{testPod("a", v1.PodRunning, true), testPod("b", v1.PodRunning, true)},
			numberOfPods: 1,
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := &DefaultKubernetesHelperImpl{Client: fake.NewClientBuilder().WithObjects(tt.pods...).Build()}
			got, err := helper.checkPodsByLabel(testLabels, testNamespace, tt.numberOfPods, v1.PodRunning,
				func(status v1.ContainerStatus) (bool, error) { return status.Ready, nil })
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("checkPodsByLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckPodsCompletedByLabelKeepsFailedPods(t *testing.T) {
	helper := &DefaultKubernetesHelperImpl{Client: fake.NewClientBuilder().WithObjects(
		testPod("failed", v1.PodFailed, false),
		testPod("done", v1.PodSucceeded, false),
	).Build()}
	got, err := helper.checkPodsByLabel(testLabels, testNamespace, 1, v1.PodSucceeded,
		func(status v1.ContainerStatus) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("a failed pod must still block the completion wait")
	}
}
