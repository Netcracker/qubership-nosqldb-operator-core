package core

import (
	"context"
	"strings"
	"time"

	"github.com/Netcracker/qubership-nosqldb-operator-core/pkg/types"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRStatusHandler interface {
	SetCRCondition(conditionStatus bool, statusType string, err error, reason string) CRStatusHandler
	SetDRStatus(status string) CRStatusHandler
	Commit() error
}

type DefaultCRStatusHandler struct {
	CRStatusHandler
	Reconciler CommonReconciler
	KubeClient client.Client
}

func (h DefaultCRStatusHandler) SetCRCondition(conditionStatus bool, statusType string, err error, reason string) CRStatusHandler {

	transitionTime := v12.Time{Time: time.Now()}
	conditionMsg := ""
	if err != nil {
		conditionMsg = err.Error()
	}

	condition := types.ServiceStatusCondition{
		Type:               statusType,
		Status:             conditionStatus,
		LastTransitionTime: transitionTime,
		Reason:             reason,
		Message:            strings.ReplaceAll(conditionMsg, "\t", " "),
	}
	h.Reconciler.UpdateStatus(condition)
	return h
}

func (h DefaultCRStatusHandler) SetDRStatus(status string) CRStatusHandler {
	drStatus := types.DisasterRecoveryStatus{
		Status: status,
	}

	h.Reconciler.UpdateDRStatus(drStatus)
	return h
}

func (h DefaultCRStatusHandler) Commit() error {
	return h.KubeClient.Status().Update(context.TODO(), h.Reconciler.GetInstance())
}
