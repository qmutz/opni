package multiclusterrolebinding

import (
	"fmt"

	_ "embed" // embed should be a blank import

	opensearchtypes "github.com/rancher/opni/pkg/opensearch/opensearch/types"
	"github.com/rancher/opni/pkg/resources/opnicluster/elastic/indices"
	"k8s.io/utils/pointer"
)

const (
	LogPolicyName        = "log-policy"
	LogIndexPrefix       = "logs-v0.5.4"
	LogIndexAlias        = "logs"
	LogIndexTemplateName = "logs_rollover_mapping"

	tracingPolicyName           = "tracing-policy"
	spanIndexPrefix             = "otel-v1-apm-span"
	spanIndexAlias              = "otel-v1-apm-span"
	spanIndexTemplateName       = "span-mapping"
	serviceMapIndexName         = "otel-v1-apm-service-map"
	serviceMapTemplateName      = "servicemap-mapping"
	preProcessingPipelineName   = "opni-ingest-pipeline"
	kibanaDashboardVersionDocID = "latest"
	kibanaDashboardVersion      = "v0.5.4"
	kibanaDashboardVersionIndex = "opni-dashboard-version"
)

var (
	oldTracingIndexPrefixes = []string{}
	OldIndexPrefixes        = []string{
		"logs-v0.1.3*",
		"logs-v0.5.1*",
	}
	DefaultRetry = opensearchtypes.RetrySpec{
		Count:   3,
		Backoff: "exponential",
		Delay:   "1m",
	}
	OpniLogPolicy = opensearchtypes.ISMPolicySpec{
		ISMPolicyIDSpec: &opensearchtypes.ISMPolicyIDSpec{
			PolicyID:   LogPolicyName,
			MarshallID: false,
		},
		Description:  "Opni policy with hot-warm-cold workflow",
		DefaultState: "hot",
		States: []opensearchtypes.StateSpec{
			{
				Name: "hot",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Rollover: &opensearchtypes.RolloverOperation{
								MinIndexAge: "1d",
								MinSize:     "20gb",
							},
						},
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "warm",
					},
				},
			},
			{
				Name: "warm",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ReplicaCount: &opensearchtypes.ReplicaCountOperation{
								NumberOfReplicas: 0,
							},
						},
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							IndexPriority: &opensearchtypes.IndexPriorityOperation{
								Priority: 50,
							},
						},
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ForceMerge: &opensearchtypes.ForceMergeOperation{
								MaxNumSegments: 1,
							},
						},
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "cold",
						Conditions: &opensearchtypes.ConditionSpec{
							MinIndexAge: "2d",
						},
					},
				},
			},
			{
				Name: "cold",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ReadOnly: &opensearchtypes.ReadOnlyOperation{},
						},
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "delete",
						Conditions: &opensearchtypes.ConditionSpec{
							MinIndexAge: "7d",
						},
					},
				},
			},
			{
				Name: "delete",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Delete: &opensearchtypes.DeleteOperation{},
						},
					},
				},
				Transitions: make([]opensearchtypes.TransitionSpec, 0),
			},
		},
		ISMTemplate: []opensearchtypes.ISMTemplateSpec{
			{
				IndexPatterns: []string{
					fmt.Sprintf("%s*", LogIndexPrefix),
				},
				Priority: 100,
			},
		},
	}

	clusterIndexRole = opensearchtypes.RoleSpec{
		RoleName: "cluster_index",
		ClusterPermissions: []string{
			"cluster_composite_ops",
			"cluster_monitor",
		},
		IndexPermissions: []opensearchtypes.IndexPermissionSpec{
			{
				IndexPatterns: []string{
					"logs*",
					serviceMapIndexName,
					fmt.Sprintf("%s*", spanIndexPrefix),
				},
				AllowedActions: []string{
					"index",
					"indices:admin/get",
					"indices:admin/mapping/put",
				},
			},
		},
	}

	opniSpanTemplate = opensearchtypes.IndexTemplateSpec{
		TemplateName: spanIndexTemplateName,
		IndexPatterns: []string{
			fmt.Sprintf("%s*", spanIndexPrefix),
		},
		Template: opensearchtypes.TemplateSpec{
			Settings: opensearchtypes.TemplateSettingsSpec{
				NumberOfShards:   1,
				NumberOfReplicas: 1,
				ISMPolicyID:      tracingPolicyName,
				RolloverAlias:    spanIndexAlias,
			},
			Mappings: opensearchtypes.TemplateMappingsSpec{
				DateDetection: pointer.BoolPtr(false),
				DynamicTemplates: []map[string]opensearchtypes.DynamicTemplateSpec{
					{
						"resource_attributes_map": opensearchtypes.DynamicTemplateSpec{
							Mapping: opensearchtypes.PropertySettings{
								Type: "keyword",
							},
							PathMatch: "resource.attributes.*",
						},
					},
					{
						"span_attributes_map": opensearchtypes.DynamicTemplateSpec{
							Mapping: opensearchtypes.PropertySettings{
								Type: "keyword",
							},
							PathMatch: "span.attributes.*",
						},
					},
				},
				Properties: map[string]opensearchtypes.PropertySettings{
					"cluster_id": {
						Type: "keyword",
					},
					"traceId": {
						IgnoreAbove: 256,
						Type:        "keyword",
					},
					"spanId": {
						IgnoreAbove: 256,
						Type:        "keyword",
					},
					"parentSpanId": {
						IgnoreAbove: 256,
						Type:        "keyword",
					},
					"name": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
					"traceGroup": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
					"traceGroupFields": {
						Properties: map[string]opensearchtypes.PropertySettings{
							"endTime": {
								Type: "date_nanos",
							},
							"durationInNanos": {
								Type: "long",
							},
							"statusCode": {
								Type: "integer",
							},
						},
					},
					"kind": {
						IgnoreAbove: 128,
						Type:        "keyword",
					},
					"startTime": {
						Type: "date_nanos",
					},
					"endTime": {
						Type: "date_nanos",
					},
					"status": {
						Properties: map[string]opensearchtypes.PropertySettings{
							"code": {
								Type: "integer",
							},
							"message": {
								Type: "keyword",
							},
						},
					},
					"serviceName": {
						Type: "keyword",
					},
					"durationInNanos": {
						Type: "long",
					},
					"events": {
						Type: "nested",
						Properties: map[string]opensearchtypes.PropertySettings{
							"time": {
								Type: "date_nanos",
							},
						},
					},
					"links": {
						Type: "nested",
					},
				},
			},
		},
		Priority: 100,
	}

	opniServiceMapTemplate = opensearchtypes.IndexTemplateSpec{
		TemplateName: serviceMapTemplateName,
		IndexPatterns: []string{
			serviceMapIndexName,
		},
		Template: opensearchtypes.TemplateSpec{
			Settings: opensearchtypes.TemplateSettingsSpec{
				NumberOfShards:   1,
				NumberOfReplicas: 1,
			},
			Mappings: opensearchtypes.TemplateMappingsSpec{
				DateDetection: pointer.BoolPtr(false),
				DynamicTemplates: []map[string]opensearchtypes.DynamicTemplateSpec{
					{
						"strings_as_keyword": {
							Mapping: opensearchtypes.PropertySettings{
								IgnoreAbove: 1024,
								Type:        "keyword",
							},
							MatchMappingType: "string",
						},
					},
				},
				Properties: map[string]opensearchtypes.PropertySettings{
					"cluster_id": {
						Type: "keyword",
					},
					"hashId": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
					"serviceName": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
					"kind": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
					"destination": {
						Properties: map[string]opensearchtypes.PropertySettings{
							"domain": {
								IgnoreAbove: 1024,
								Type:        "keyword",
							},
							"resource": {
								IgnoreAbove: 1024,
								Type:        "keyword",
							},
						},
					},
					"target": {
						Properties: map[string]opensearchtypes.PropertySettings{
							"domain": {
								IgnoreAbove: 1024,
								Type:        "keyword",
							},
							"resource": {
								IgnoreAbove: 1024,
								Type:        "keyword",
							},
						},
					},
					"traceGroupName": {
						IgnoreAbove: 1024,
						Type:        "keyword",
					},
				},
			},
		},
	}

	preprocessingPipeline = opensearchtypes.IngestPipeline{
		Description: "Opni preprocessing ingest pipeline",
		Processors: []opensearchtypes.Processor{
			{
				OpniLoggingProcessor: &opensearchtypes.OpniProcessorConfig{},
			},
			{
				OpniPreProcessor: &opensearchtypes.OpniProcessorConfig{},
			},
		},
	}

	OpniLogTemplate = opensearchtypes.IndexTemplateSpec{
		TemplateName: LogIndexTemplateName,
		IndexPatterns: []string{
			fmt.Sprintf("%s*", LogIndexPrefix),
		},
		Template: opensearchtypes.TemplateSpec{
			Settings: opensearchtypes.TemplateSettingsSpec{
				NumberOfShards:   1,
				NumberOfReplicas: 1,
				ISMPolicyID:      LogPolicyName,
				RolloverAlias:    LogIndexAlias,
				DefaultPipeline:  preProcessingPipelineName,
			},
			Mappings: opensearchtypes.TemplateMappingsSpec{
				Properties: map[string]opensearchtypes.PropertySettings{
					"timestamp": {
						Type: "date",
					},
					"time": {
						Type: "date",
					},
					"log": {
						Type: "text",
					},
					"masked_log": {
						Type: "text",
					},
					"log_type": {
						Type: "keyword",
					},
					"kubernetes_component": {
						Type: "keyword",
					},
					"cluster_id": {
						Type: "keyword",
					},
					"anomaly_level": {
						Type: "keyword",
					},
				},
			},
		},
		Priority: 100,
	}

	// kibanaObjects contains the ndjson form data for creating the kibana
	// index patterns and dashboards
	//go:embed dashboard.ndjson
	kibanaObjects string
)

func (r *Reconciler) logISMPolicy() opensearchtypes.ISMPolicySpec {
	return opensearchtypes.ISMPolicySpec{
		ISMPolicyIDSpec: &opensearchtypes.ISMPolicyIDSpec{
			PolicyID:   LogPolicyName,
			MarshallID: false,
		},
		Description:  "Opni policy with hot-warm-cold workflow",
		DefaultState: "hot",
		States: []opensearchtypes.StateSpec{
			{
				Name: "hot",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Rollover: &opensearchtypes.RolloverOperation{
								MinIndexAge: "1d",
								MinSize:     "20gb",
							},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "warm",
					},
				},
			},
			{
				Name: "warm",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ReplicaCount: &opensearchtypes.ReplicaCountOperation{
								NumberOfReplicas: 0,
							},
						},
						Retry: &indices.DefaultRetry,
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							IndexPriority: &opensearchtypes.IndexPriorityOperation{
								Priority: 50,
							},
						},
						Retry: &indices.DefaultRetry,
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ForceMerge: &opensearchtypes.ForceMergeOperation{
								MaxNumSegments: 1,
							},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "delete",
						Conditions: &opensearchtypes.ConditionSpec{
							MinIndexAge: func() string {
								if r.multiClusterRoleBinding.Spec.OpensearchConfig != nil {
									return r.multiClusterRoleBinding.Spec.OpensearchConfig.IndexRetention
								}
								return "7d"
							}(),
						},
					},
				},
			},
			{
				Name: "delete",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Delete: &opensearchtypes.DeleteOperation{},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: make([]opensearchtypes.TransitionSpec, 0),
			},
		},
		ISMTemplate: []opensearchtypes.ISMTemplateSpec{
			{
				IndexPatterns: []string{
					fmt.Sprintf("%s*", LogIndexPrefix),
				},
				Priority: 100,
			},
		},
	}
}

func (r *Reconciler) traceISMPolicy() opensearchtypes.ISMPolicySpec {
	return opensearchtypes.ISMPolicySpec{
		ISMPolicyIDSpec: &opensearchtypes.ISMPolicyIDSpec{
			PolicyID:   tracingPolicyName,
			MarshallID: false,
		},
		Description:  "Opni policy with hot-warm-cold workflow",
		DefaultState: "hot",
		States: []opensearchtypes.StateSpec{
			{
				Name: "hot",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Rollover: &opensearchtypes.RolloverOperation{
								MinIndexAge: "1d",
								MinSize:     "20gb",
							},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "warm",
					},
				},
			},
			{
				Name: "warm",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ReplicaCount: &opensearchtypes.ReplicaCountOperation{
								NumberOfReplicas: 0,
							},
						},
						Retry: &indices.DefaultRetry,
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							IndexPriority: &opensearchtypes.IndexPriorityOperation{
								Priority: 50,
							},
						},
						Retry: &indices.DefaultRetry,
					},
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ForceMerge: &opensearchtypes.ForceMergeOperation{
								MaxNumSegments: 1,
							},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "cold",
						Conditions: &opensearchtypes.ConditionSpec{
							MinIndexAge: "2d",
						},
					},
				},
			},
			{
				Name: "cold",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							ReadOnly: &opensearchtypes.ReadOnlyOperation{},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: []opensearchtypes.TransitionSpec{
					{
						StateName: "delete",
						Conditions: &opensearchtypes.ConditionSpec{
							MinIndexAge: func() string {
								if r.multiClusterRoleBinding.Spec.OpensearchConfig != nil {
									return r.multiClusterRoleBinding.Spec.OpensearchConfig.IndexRetention
								}
								return "7d"
							}(),
						},
					},
				},
			},
			{
				Name: "delete",
				Actions: []opensearchtypes.ActionSpec{
					{
						ActionOperation: &opensearchtypes.ActionOperation{
							Delete: &opensearchtypes.DeleteOperation{},
						},
						Retry: &indices.DefaultRetry,
					},
				},
				Transitions: make([]opensearchtypes.TransitionSpec, 0),
			},
		},
		ISMTemplate: []opensearchtypes.ISMTemplateSpec{
			{
				IndexPatterns: []string{
					fmt.Sprintf("%s*", spanIndexPrefix),
				},
				Priority: 100,
			},
		},
	}
}
