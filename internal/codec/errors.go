package codec

import "fmt"

// ExceptionCode is the code carried by a Modbus exception response.
type ExceptionCode byte

// Known exception codes. Written in decimal on purpose (see
// scripts/check-readonly.sh).
const (
	ExcIllegalFunction        ExceptionCode = 1
	ExcIllegalDataAddress     ExceptionCode = 2
	ExcIllegalDataValue       ExceptionCode = 3
	ExcServerDeviceFailure    ExceptionCode = 4
	ExcAcknowledge            ExceptionCode = 5
	ExcServerDeviceBusy       ExceptionCode = 6
	ExcGatewayPathUnavailable ExceptionCode = 10
	ExcGatewayTargetNoReply   ExceptionCode = 11
)

// Name returns the exception name shown in the UI.
func (c ExceptionCode) Name() string {
	switch c {
	case ExcIllegalFunction:
		return "Função ilegal"
	case ExcIllegalDataAddress:
		return "Endereço ilegal"
	case ExcIllegalDataValue:
		return "Valor ilegal"
	case ExcServerDeviceFailure:
		return "Falha no dispositivo"
	case ExcAcknowledge:
		return "Confirmação (ACK)"
	case ExcServerDeviceBusy:
		return "Dispositivo ocupado"
	case ExcGatewayPathUnavailable:
		return "Caminho de gateway indisponível"
	case ExcGatewayTargetNoReply:
		return "Dispositivo atrás do gateway não respondeu"
	}
	return fmt.Sprintf("Exceção desconhecida (0x%02X)", byte(c))
}

// Hint returns advice for the user, or "" when there is none.
func (c ExceptionCode) Hint() string {
	switch c {
	case ExcIllegalFunction:
		return "O dispositivo não suporta esta função nesta tabela. Verifique se a tabela está correta."
	case ExcIllegalDataAddress:
		return "Causas comuns: tabela errada (3xxxx × 4xxxx) ou convenção base 0/base 1 trocada. " +
			"Compare com os vizinhos na lista, confira o manual e reinicie com a faixa ou --convention corrigida."
	case ExcIllegalDataValue:
		return "A quantidade pedida pode ser grande demais para o dispositivo."
	case ExcServerDeviceFailure:
		return "Erro interno do escravo."
	case ExcAcknowledge:
		return "Comando aceito, processamento demorado."
	case ExcServerDeviceBusy:
		return "Tente um intervalo maior."
	case ExcGatewayPathUnavailable:
		return "O gateway não encontrou a rota para o escravo."
	case ExcGatewayTargetNoReply:
		return "Verifique o unit ID e a comunicação serial do gateway."
	}
	return ""
}

// ExceptionError is an exception response from the device.
type ExceptionError struct {
	Function byte
	Code     ExceptionCode
}

// Error returns e.g. "exceção 02 · endereço ilegal".
func (e *ExceptionError) Error() string {
	name := []rune(e.Code.Name())
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		name[0] += 'a' - 'A'
	}
	return fmt.Sprintf("exceção %02X · %s", byte(e.Code), string(name))
}

// CRCError is an RTU frame whose CRC does not match its contents.
type CRCError struct {
	Got, Want uint16
}

func (e *CRCError) Error() string {
	return "CRC inválido"
}

// Field identifies what did not match in a MismatchError.
type Field string

// Fields checked against the request.
const (
	FieldTransaction Field = "transaction ID"
	FieldUnit        Field = "unit ID"
	FieldSlave       Field = "endereço do escravo"
	FieldFunction    Field = "função"
)

// MismatchError is a response that does not belong to the request:
// different transaction ID, unit ID, slave address or function.
type MismatchError struct {
	Field     Field
	Want, Got int
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("resposta não corresponde ao pedido: %s %d (esperado %d)", e.Field, e.Got, e.Want)
}

// MalformedError is a response with incoherent sizes or fields.
type MalformedError struct {
	Reason string
}

func (e *MalformedError) Error() string {
	return "resposta malformada: " + e.Reason
}
