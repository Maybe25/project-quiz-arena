// Package wsapi contiene helpers para interactuar con API Gateway WebSocket.
//
// El servicio clave es PostToConnection: dado un connectionId, envía un mensaje
// JSON al cliente WebSocket que tiene esa conexión abierta.
//
// CONCEPTO GO — Interfaces:
// Definimos una interface "Poster" con un solo método. Cualquier tipo que tenga
// ese método "es" un Poster, sin necesidad de declararlo explícitamente.
// Esto nos permite pasar un mock en tests y el cliente real en Lambda.
// En Python equivaldría a duck typing, pero verificado en compile time.
package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
)

// Poster es la interface que abstrae el envío de mensajes WebSocket.
// Cualquier tipo con el método PostMessage satisface esta interface.
type Poster interface {
	PostMessage(ctx context.Context, connectionID string, payload interface{}) error
}

// Client implementa Poster usando el SDK real de AWS.
type Client struct {
	api *apigatewaymanagementapi.Client
}

// NewClient crea un cliente listo para enviar mensajes WebSocket.
//
// El endpoint tiene el formato:
//   https://<api-id>.execute-api.<region>.amazonaws.com/<stage>
//
// Terraform lo inyecta como variable de entorno WS_ENDPOINT.
func NewClient(ctx context.Context) (*Client, error) {
	endpoint := os.Getenv("WS_ENDPOINT")
	if endpoint == "" {
		return nil, fmt.Errorf("WS_ENDPOINT env var not set")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// apigatewaymanagementapi es el cliente específico para PostToConnection.
	// Necesita el endpoint de tu API Gateway WebSocket como base URL.
	api := apigatewaymanagementapi.NewFromConfig(cfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	return &Client{api: api}, nil
}

// PostMessage serializa payload a JSON y lo envía al connectionId dado.
//
// CONCEPTO GO — %w en fmt.Errorf:
// Wrappea el error original para que pueda inspeccionarse con errors.Is/As.
// Equivalente aproximado en Python: raise RuntimeError("contexto") from original_error
func (c *Client) PostMessage(ctx context.Context, connectionID string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	_, err = c.api.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         data,
	})
	if err != nil {
		return fmt.Errorf("post to connection %s: %w", connectionID, err)
	}

	return nil
}
