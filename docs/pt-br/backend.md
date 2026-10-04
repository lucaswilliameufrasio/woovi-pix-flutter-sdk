# Contrato e segurança do backend

[Início](../../README.md) · [English](../en-us/backend.md)

O Flutter exibe o checkout. O servidor do lojista controla autorização, valor
do pedido e confirmação de pagamento. A entrega do produto é responsabilidade
do lojista e não é implementada neste exemplo.

## Rotas do exemplo

| Método e caminho | Finalidade | Proteção |
| --- | --- | --- |
| `POST /v1/checkout-sessions` | Criar/recuperar o checkout simulado com `order_id`. | Exclusivo do fluxo simulado. |
| `POST /v1/merchant/checkout-sessions` | Criar/recuperar checkout de cobrança do lojista. | Autorização do lojista e `Idempotency-Key`. |
| `GET /v1/checkout-sessions/{id}` | Consultar o estado do checkout. | Token bearer limitado ao checkout. |
| `POST /v1/demo/checkouts/{id}/pay` | Simular confirmação do PSP. | Sem autenticação; exige `ENABLE_DEMO_PSP=true`, apenas local. |
| `POST /v1/webhooks/woovi` | Receber notificações Woovi. | Habilitação explícita e assinatura RSA. |

As rotas usam `/v1`. Os corpos JSON usam `snake_case`, exceto os eventos webhook
da Woovi, que seguem os nomes definidos pelo provedor.
O modo sandbox não disponibiliza a rota simulada de criação.

## Autorização e idempotência

O fluxo do lojista exige `MerchantCheckoutAuthorizer`, PostgreSQL e um
`ChargeCreator` no servidor. O autorizador/armazenamento determina o pedido e
seu valor; **não aceite o valor informado pelo Flutter como fonte confiável**.
O sandbox configura explicitamente um adaptador de autenticação somente para testes.

| Situação | Comportamento |
| --- | --- |
| Mesma `Idempotency-Key` e mesmo pedido | Recupera o mesmo checkout. |
| Mesma chave em outro pedido | Retorna conflito. |
| Nova tentativa | Exige `EXPIRED` confirmado no PSP. |
| Pedido já pago | Não admite outra cobrança. |
| PSP retorna `COMPLETED` | Sessão `paid`, sem `pix_copy_paste`. |
| Timeout ou resposta ambígua | Recuperação por GET, sem repetir o POST ao PSP. |

Autenticação sandbox não substitui autenticação de produção nem validação de
propriedade do pedido. Erros de rede não são estados de pagamento.

## Erros HTTP

Formato: `{ message, error_code, extra? }`. O `error_code` é estável;
`message` é uma mensagem legível para humanos.

| HTTP | Significado |
| --- | --- |
| 400 | JSON malformado. |
| 422 | Falha de validação semântica. |
| 404 | Recurso não encontrado. |
| 500 | Erro inesperado, com resposta genérica. |

## Criação de cobrança e reconciliação

`WooviChargeClient` usa `Authorization: <AppID>` e valores em centavos:

- Criação: `POST /api/v1/charge`.
- Consulta: `GET /api/v1/charge/{id}`, com identificador de cobrança ou `correlationID`.

`SubmitChargeForOrder` coordena reserva persistente, transição exclusiva
`reserved` → `submitting`, um único POST e persistência do resultado.
O adaptador do lojista vincula a operação à chave de idempotência e cria a sessão
a partir do registro autoritativo do PSP.

- Resultados ambíguos ficam como `unknown` no PostgreSQL e não repetem o POST.
- A reconciliação usa concessões com expiração e `SKIP LOCKED`.
- Falhas de GET usam espera exponencial limitada com variação aleatória.
- O processo independente de reconciliação usa somente GET, tem limites
  conservadores e fica desabilitado por padrão.
- Chamadas concorrentes e falhas do PSP são testadas com PostgreSQL real e
  servidor HTTP local de teste, sem chamadas externas.

## Verificação de webhook

O receptor verifica `x-webhook-signature` com as chaves públicas RSA da Woovi:

1. Verifica a assinatura sobre o corpo bruto.
2. Elimina eventos duplicados atomicamente.
3. Confere identificador de correlação e valor em centavos.
4. Aplica eventos compatíveis de cobrança concluída/expirada.

Eventos não suportados são ignorados. Eventos assinados inconsistentes ficam
em quarentena com HTTP 202. O sandbox pode usar seu próprio endpoint de chaves;
veja a [configuração de webhook](sandbox.md#webhook-opcional).

## O que isso não garante

- AppID é exclusivo do servidor; nem o callback mobile nem o webhook, isoladamente,
  autorizam a entrega de pedidos em produção.
- O PostgreSQL persiste checkout, token, evento e tentativa. O armazenamento em
  memória é apenas uma alternativa para a simulação padrão.
- A semântica de consulta/estado das cobranças e as garantias de idempotência
  do PSP ainda precisam de verificação externa.
- Nenhuma chamada à API de produção ou sandbox foi realizada durante o desenvolvimento.
  Os testes locais não tornam o exemplo pronto para produção.

## Referências Woovi consultadas

| Documentação | Contrato descrito |
| --- | --- |
| [Autenticação](https://developers.woovi.com/en/docs/apis/api-getting-started) | AppID em `Authorization`; HTTPS obrigatório. |
| [Criação de cobrança](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) | `POST /api/v1/charge`, centavos e `correlationID`. |
| [Cobrança concluída](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) | `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, `COMPLETED`, `pix.status=CONFIRMED`. |
| [Chaves públicas](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) | `base64(RSA-SHA256(raw body))`, rotação e cache de uma hora com uso do cache anterior em caso de falha. |
| [Novas tentativas de webhook](https://developers.woovi.com/en/docs/webhook/webhook-retry) | 8 tentativas; repetição em HTTP >400 ou indisponibilidade. |
