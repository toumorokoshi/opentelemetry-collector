// Copyright 2020, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trace

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"testing"

	occommon "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	ocresource "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	octrace "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	collectortrace "github.com/open-telemetry/opentelemetry-proto/gen/go/collector/trace/v1"
	otlpcommon "github.com/open-telemetry/opentelemetry-proto/gen/go/common/v1"
	otlpresource "github.com/open-telemetry/opentelemetry-proto/gen/go/resource/v1"
	otlptrace "github.com/open-telemetry/opentelemetry-proto/gen/go/trace/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-collector/consumer"
	"github.com/open-telemetry/opentelemetry-collector/consumer/consumerdata"
	"github.com/open-telemetry/opentelemetry-collector/internal"
	"github.com/open-telemetry/opentelemetry-collector/internal/data"
	"github.com/open-telemetry/opentelemetry-collector/observability"
	"github.com/open-telemetry/opentelemetry-collector/receiver/receivertest"
	"github.com/open-telemetry/opentelemetry-collector/testutils"
)

var _ collectortrace.TraceServiceServer = (*Receiver)(nil)

func TestExport(t *testing.T) {
	// given

	traceSink := receivertest.NewTraceAppender()

	_, port, doneFn := otlpReceiverOnGRPCServer(t, traceSink)
	defer doneFn()

	traceClient, traceClientDoneFn, err := makeTraceServiceClient(port)
	require.NoError(t, err, "Failed to create the TraceServiceClient: %v", err)
	defer traceClientDoneFn()

	// when

	unixnanos := uint64(12578940000000012345)

	traceID, err := base64.StdEncoding.DecodeString("SEhaOVO7YSQ=")
	assert.NoError(t, err)

	spanID, err := base64.StdEncoding.DecodeString("QuHicGYRg4U=")
	assert.NoError(t, err)

	req := &collectortrace.ExportTraceServiceRequest{
		ResourceSpans: []*otlptrace.ResourceSpans{
			{
				Resource: &otlpresource.Resource{
					Attributes: []*otlpcommon.AttributeKeyValue{
						{
							Key:         "key1",
							StringValue: "value1",
						},
					},
				},
				InstrumentationLibrarySpans: []*otlptrace.InstrumentationLibrarySpans{
					{
						InstrumentationLibrary: &otlpcommon.InstrumentationLibrary{
							Name:    "name1",
							Version: "version1",
						},
						Spans: []*otlptrace.Span{
							{
								TraceId:           traceID,
								SpanId:            spanID,
								Name:              "operationB",
								Kind:              otlptrace.Span_SERVER,
								StartTimeUnixNano: unixnanos,
								EndTimeUnixNano:   unixnanos,
								Events: []*otlptrace.Span_Event{
									{
										TimeUnixNano: unixnanos,
										Name:         "event1",
										Attributes: []*otlpcommon.AttributeKeyValue{
											{
												Key:         "eventattr1",
												Type:        otlpcommon.AttributeKeyValue_STRING,
												StringValue: "eventattrval1",
											},
										},
										DroppedAttributesCount: 4,
									},
								},
								Links: []*otlptrace.Span_Link{
									{
										TraceId: traceID,
										SpanId:  spanID,
									},
								},
								DroppedAttributesCount: 1,
								DroppedEventsCount:     2,
								Status:                 &otlptrace.Status{Message: "status-cancelled", Code: otlptrace.Status_Cancelled},
								TraceState:             "a=text,b=123",
							},
						},
					},
				},
			},
		},
	}

	resp, err := traceClient.Export(context.Background(), req)
	require.NoError(t, err, "Failed to export trace: %v", err)
	require.NotNil(t, resp, "The response is missing")

	// assert

	require.Equal(t, 1, len(traceSink.TraceDataList),
		"unexpected length: %v", len(traceSink.TraceDataList))

	traceData := consumerdata.TraceData{
		Node: &occommon.Node{},
		Resource: &ocresource.Resource{
			Labels: map[string]string{
				"key1": "value1",
			},
		},
		Spans: []*octrace.Span{
			{
				TraceId:   traceID,
				SpanId:    spanID,
				Name:      &octrace.TruncatableString{Value: "operationB"},
				Kind:      octrace.Span_SERVER,
				StartTime: internal.UnixNanoToTimestamp(data.TimestampUnixNano(unixnanos)),
				EndTime:   internal.UnixNanoToTimestamp(data.TimestampUnixNano(unixnanos)),
				TimeEvents: &octrace.Span_TimeEvents{
					TimeEvent: []*octrace.Span_TimeEvent{
						{
							Time: internal.UnixNanoToTimestamp(data.TimestampUnixNano(unixnanos)),
							Value: &octrace.Span_TimeEvent_Annotation_{
								Annotation: &octrace.Span_TimeEvent_Annotation{
									Description: &octrace.TruncatableString{Value: "event1"},
									Attributes: &octrace.Span_Attributes{
										AttributeMap: map[string]*octrace.AttributeValue{
											"eventattr1": {
												Value: &octrace.AttributeValue_StringValue{
													StringValue: &octrace.TruncatableString{Value: "eventattrval1"},
												},
											},
										},
										DroppedAttributesCount: 4,
									},
								},
							},
						},
					},
					DroppedMessageEventsCount: 2,
				},
				Links: &octrace.Span_Links{
					Link: []*octrace.Span_Link{
						{
							TraceId: traceID,
							SpanId:  spanID,
						},
					},
				},
				Attributes: &octrace.Span_Attributes{
					DroppedAttributesCount: 1,
				},
				Status: &octrace.Status{Message: "status-cancelled", Code: 1},
				Tracestate: &octrace.Span_Tracestate{
					Entries: []*octrace.Span_Tracestate_Entry{
						{
							Key:   "a",
							Value: "text",
						},
						{
							Key:   "b",
							Value: "123",
						},
					},
				},
			},
		},
		SourceFormat: "otlp_trace",
	}

	assert.EqualValues(t, traceData, traceSink.TraceDataList[0])
}

func makeTraceServiceClient(port int) (collectortrace.TraceServiceClient, func(), error) {
	addr := fmt.Sprintf(":%d", port)
	cc, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, nil, err
	}

	metricsClient := collectortrace.NewTraceServiceClient(cc)

	doneFn := func() { _ = cc.Close() }
	return metricsClient, doneFn, nil
}

func otlpReceiverOnGRPCServer(t *testing.T, tc consumer.TraceConsumer) (r *Receiver, port int, done func()) {
	ln, err := net.Listen("tcp", "localhost:")
	require.NoError(t, err, "Failed to find an available address to run the gRPC server: %v", err)

	doneFnList := []func(){func() { ln.Close() }}
	done = func() {
		for _, doneFn := range doneFnList {
			doneFn()
		}
	}

	_, port, err = testutils.HostPortFromAddr(ln.Addr())
	if err != nil {
		done()
		t.Fatalf("Failed to parse host:port from listener address: %s error: %v", ln.Addr(), err)
	}

	r, err = New(receiverTagValue, tc)
	require.NoError(t, err, "Failed to create the Receiver: %v", err)

	// Now run it as a gRPC server
	srv := observability.GRPCServerWithObservabilityEnabled()
	collectortrace.RegisterTraceServiceServer(srv, r)
	go func() {
		_ = srv.Serve(ln)
	}()

	return r, port, done
}
