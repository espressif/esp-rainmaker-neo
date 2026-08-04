// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"time"
)

func CreateResponse(messageID, namespace, name string, payload interface{}, correlationID string, endpoint *Endpoint) AlexaResponse {
	var response AlexaResponse
	response.Event.Header.MessageID = messageID
	response.Event.Header.Namespace = namespace
	response.Event.Header.Name = name
	response.Event.Header.PayloadVersion = "3"
	response.Event.Header.CorrelationID = correlationID
	var p interface{} = payload
	response.Event.Payload = &p
	response.Event.Endpoint = endpoint
	return response
}

func CreateResponseFromReq(request *AlexaRequest, namespace, name string, payload interface{}) AlexaResponse {
	response := CreateResponse(request.Directive.Header.MessageID, namespace, name, payload, request.Directive.Header.CorrelationID, request.Directive.Endpoint)
	return response
}

func (r *ContextPropertyList) AddCtxProperty(nameSpace, name string, value interface{}) {
	*r = append(*r, ContextProperty{
		NameSpace:                 nameSpace,
		Name:                      name,
		Value:                     value,
		TimeOfSample:              time.Now().Format(time.RFC3339),
		UncertaintyInMilliseconds: 0,
	})
}
