# Woovi Pix Flutter SDK

[English (en-US)](README-en-us.md)

Pacote Flutter e servidor de exemplo do lojista para exibir um checkout Pix. O aplicativo Flutter se comunica somente com o servidor do lojista; as credenciais Woovi e a confirmação do pagamento permanecem no servidor.

> **Exemplo, não destinado à produção:** o fluxo padrão usa um provedor de serviços de pagamento (PSP) simulado. O fluxo sandbox, habilitado explicitamente, cria cobranças de teste na Woovi, nunca na API de produção. Nenhuma chamada ao sandbox ou à produção foi executada durante o desenvolvimento. As credenciais permanecem no servidor. O lojista precisa implementar autenticação e liberação do pedido para produção.

## Estrutura do repositório

- `packages/woovi_pix_flutter`: pacote Flutter reutilizável com controlador, transporte HTTP, modelo e interface de checkout.
- `example/`: aplicativo Flutter de demonstração executável.
- `examples/backend`: servidor Go do lojista, migrações PostgreSQL e simulador local de PSP.

## Requisitos

- Flutter 3.24+ / Dart 3.5+
- Go 1.27.1 (versão fixada em `.mise.toml` e `examples/backend/go.mod`)
- PostgreSQL 18 (imagem fixada por resumo criptográfico em `examples/backend/docker-compose.yml`)

## Executar a demonstração

Inicie o serviço PostgreSQL local isolado e o servidor (as migrações são aplicadas na inicialização):

```sh
cd examples/backend
mise install
docker compose -p woovi-pix-flutter-sdk-demo up -d postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-demo port postgres 5432 | sed 's/.*://')"
DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable" ENABLE_DEMO_PSP=true mise exec -- go run ./cmd/server
```

Em outro terminal:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

A URL padrão é para o emulador Android. No simulador iOS, use `--dart-define=DEMO_BACKEND=http://127.0.0.1:8080`. O uso de HTTP é restrito ao desenvolvimento local.

O aplicativo cria um pedido fictício pelo servidor, exibe o QR Pix simulado e consulta periodicamente o estado do checkout. Os endpoints da API são versionados em `/v1`; os corpos JSON usam `snake_case`, exceto as notificações webhook recebidas da Woovi, cujos nomes de campos são definidos pela Woovi. Com `ENABLE_DEMO_PSP=true`, use `POST /v1/demo/checkouts/{id}/pay` para simular a confirmação do PSP. Essa rota sem autenticação fica desabilitada por padrão, nunca deve ser habilitada fora das demonstrações locais e não pode ser habilitada junto com o receptor de webhook. O callback de pagamento da interface é apenas um sinal de apresentação; a liberação do pedido pelo lojista deve consultar o estado do próprio pedido no servidor.

## Testar o pagamento simulado

No app, toque em **Pagar com Pix**. O QR é fictício: não pague com um banco.
Em outro terminal, recupere o checkout do mesmo pedido:

```sh
curl -s http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Copie checkout_id da resposta:
curl -X POST http://localhost:8080/v1/demo/checkouts/COLE_O_CHECKOUT_ID/pay
```

O app deve mostrar pagamento confirmado e esconder QR/código. O token pode ser
rotacionado no replay; tokens anteriores permanecem válidos até sua expiração.

## Testar com Woovi sandbox de verdade

### Conta e credenciais

Cadastre uma conta separada em <https://app.woovi-sandbox.com/>. Dados de produção
não funcionam no sandbox. Gere um AppID de teste nas permissões/API dessa conta.
O endpoint documentado é `https://api.woovi-sandbox.com`, sem `/api/v1` na base.
Fontes: [ambiente de teste](https://developers.woovi.com/en/docs/test-environment),
[configuração sandbox](https://developers.woovi.com/en/docs/sdk/node/how-to-configure-for-sandbox).

Não use um app bancário real para pagar. Use a conta de teste/funcionalidade de
simulação do painel Woovi sandbox para concluir a cobrança.
Prepare também a conta bancária de teste conforme
[Testando Pix sem dinheiro real](https://developers.woovi.com/en/docs/test-environment/test-account/flow-company-bank-test)
e siga [Pagando uma Cobrança Pix de Teste](https://developers.woovi.com/en/docs/test-environment/test-account/test-pay-pix)
para confirmar a cobrança criada pelo app.

### Backend (terminal 1, a partir da raiz)

Este modo usa um único pedido fixo `sandbox-order-1`, de R$ 25,99, e uma sessão
de teste compartilhada. Não é autenticação de produção. Use banco isolado, não
exponha esse servidor publicamente e não reutilize o token fora deste exemplo.

```sh
mise install
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-sandbox up -d --wait postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-sandbox port postgres 5432 | sed 's/.*://')"
export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
export ENABLE_WOOVI_SANDBOX_CHECKOUT=true
export WOOVI_API_BASE_URL=https://api.woovi-sandbox.com
export SANDBOX_SESSION_TOKEN="$(openssl rand -hex 32)"
# Bash: leitura sem eco nem credencial literal no histórico
read -rsp 'AppID do sandbox: ' WOOVI_APP_ID
export WOOVI_APP_ID
export DEMO_ADDR=127.0.0.1:8080
unset ENABLE_DEMO_PSP ENABLE_WOOVI_WEBHOOK ENABLE_WOOVI_RECONCILIATION
# Anote SANDBOX_SESSION_TOKEN para usar no terminal do app (não o AppID).
mise exec -- go run ./cmd/server
```

A configuração rejeita PostgreSQL ausente, token curto, URL diferente da base
sandbox e mistura com PSP simulado. A rota simulada de criação fica ausente.
O worker independente fica desabilitado neste exemplo: replays reconciliam
ambiguidades por GET e consultas autenticadas de status verificam o PSP por GET.

### App (terminal 2, a partir da raiz)

```sh
cd example
export SANDBOX_SESSION_TOKEN='COLE_O_TOKEN_DE_TESTE_DO_BACKEND'
export SANDBOX_IDEMPOTENCY_KEY="$(openssl rand -hex 16)"
# Anote e preserve essa chave mesmo após erro/restart do app.
mise exec -- flutter pub get
mise exec -- flutter run \
  --dart-define=WOOVI_SANDBOX=true \
  --dart-define=SANDBOX_SESSION_TOKEN="$SANDBOX_SESSION_TOKEN" \
  --dart-define=SANDBOX_IDEMPOTENCY_KEY="$SANDBOX_IDEMPOTENCY_KEY" \
  --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Toque **Pagar com Pix**: agora chama `/v1/merchant/checkout-sessions` e cria uma
cobrança no sandbox. Simule sua conclusão na Woovi: o app consulta o backend a
cada 3s; o backend consulta a cobrança via GET e confirma `paid` sem QR.
AppID nunca é enviado ao Flutter. O token da sessão de teste **é incorporado ao
app exemplo**: não distribua esse APK, nem use esse mecanismo em produção.

Se houver timeout/`PAYMENT_PROCESSING`, pressione novamente usando **a mesma
chave**. O backend não repete o POST ao PSP; tenta recuperar via GET. Não apague
o banco/tentativa para contornar ambiguidades. Somente após `EXPIRED` confirmado
no PSP use nova chave; um pedido pago não admite outra cobrança. Para uma nova
rodada independente, use outro banco isolado e outro pedido/ambiente de teste,
sem descartar uma tentativa ainda incerta. Erros de consulta não significam pago.

### iOS e dispositivos

No simulador iOS, com backend no mesmo Mac, substitua a URL por
`http://127.0.0.1:8080` e execute `mise exec -- flutter run -d ID_DO_SIMULADOR ...`.
Veja IDs com `mise exec -- flutter devices`. iOS precisa de macOS/Xcode e não
foi validado aqui. No aparelho físico, use IP LAN do host e binding apropriado;
somente em rede privada de teste (HTTP não protege o token). Preferir simulador.

### Webhook opcional

Polling GET funciona sem túnel/webhook. Para testar webhook, configure no painel
sandbox uma URL HTTPS acessível para `/v1/webhooks/woovi` e no backend:

```sh
export ENABLE_WOOVI_WEBHOOK=true
export WOOVI_WEBHOOK_PUBLIC_KEYS_URL=https://api.woovi-sandbox.com/api/v1/webhook/public-keys
```

Reinicie o backend; nunca habilite `ENABLE_DEMO_PSP` junto. Não exponha a sessão
de teste via um túnel público irrestrito; exponha apenas o caminho de webhook.
Webhooks são verificados pelo backend, e fulfillment comercial continua fora
do exemplo. Os testes automatizados usam HTTP fixture local, não a Woovi.

## Contrato do servidor do lojista

`POST /v1/checkout-sessions` é o fluxo simulado e recebe `order_id`. `POST /v1/merchant/checkout-sessions` exige um `MerchantCheckoutAuthorizer` configurado, PostgreSQL e um `ChargeCreator` no servidor; o modo sandbox configura explicitamente um adaptador exclusivo para testes. Exige `Idempotency-Key`, obtém o valor do autorizador ou armazenamento e nunca confia em um valor enviado pelo Flutter. Chaves correspondentes recuperam o mesmo checkout; reutilizar a chave entre pedidos gera conflito; novas tentativas exigem `EXPIRED` confirmado. `COMPLETED` retorna `paid` sem `pix_copy_paste`. `GET /v1/checkout-sessions/{id}` usa um token bearer limitado ao checkout. Os erros seguem `{ message, error_code, extra? }` com códigos estáveis: JSON malformado 400, validação semântica 422, recurso não encontrado 404 e erros inesperados genéricos 500. Erros de rede não são estados de pagamento. A autenticação sandbox não substitui autenticação do lojista nem validação de propriedade do pedido.

O servidor também contém um `WooviChargeClient` que implementa a criação documentada de cobranças (`POST /api/v1/charge`) e a consulta (`GET /api/v1/charge/{id}`, em que `id` pode ser o identificador da cobrança ou `correlationID`), usando `Authorization: <AppID>` e valores em centavos. `SubmitChargeForOrder` coordena uma reserva persistente, a transição exclusiva `reserved` → `submitting`, um único POST e o tratamento de persistência e ambiguidade; chamadas concorrentes e falhas do PSP são cobertas por testes com PostgreSQL e servidor HTTP local de teste. O adaptador autenticado de checkout do lojista vincula essa coordenação a uma `Idempotency-Key` persistente e cria a sessão a partir do registro autoritativo do PSP. O PostgreSQL mantém resultados ambíguos como `unknown`; essas tentativas nunca repetem o POST. A reconciliação usa concessões com expiração no PostgreSQL e `SKIP LOCKED`; consultas GET que falham agendam novas tentativas com espera exponencial limitada e variação aleatória. O processo opcional de reconciliação usa somente GET, fica desabilitado por padrão e tem limites conservadores. Mantenha o AppID exclusivamente no servidor.

O receptor opcional `POST /v1/webhooks/woovi` verifica `x-webhook-signature` usando as chaves públicas RSA da Woovi, elimina eventos brutos duplicados atomicamente e aplica eventos de cobrança concluída ou expirada quando o identificador de correlação e o valor em centavos correspondem. Eventos não suportados são ignorados; eventos assinados inconsistentes são colocados em quarentena com HTTP 202. O sandbox pode usar seu próprio endpoint de chaves, conforme documentado acima. Isso **não implementa a liberação do pedido pelo lojista**: nem o estado do webhook nem o callback do aplicativo, isoladamente, autorizam a entrega de pedidos em produção. O AppID deve permanecer no servidor. O PostgreSQL persiste os estados de checkout, token, evento e tentativa; o armazenamento em memória é exclusivo do fluxo simulado padrão. Nenhum dos modos torna este exemplo pronto para produção.

Documentação verificada: [autenticação da API](https://developers.woovi.com/en/docs/apis/api-getting-started) usa `Authorization: <AppID>` e exige HTTPS; [criação de cobrança](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) usa `POST /api/v1/charge`, valores em centavos e `correlationID`; [notificação de cobrança concluída](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) inclui o evento `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, estado `COMPLETED` e `pix.status=CONFIRMED`; [chaves públicas do webhook](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) especifica `x-webhook-signature = base64(RSA-SHA256(raw body))`, aceita rotação de chaves e orienta manter cache por uma hora, com uso do cache anterior em caso de falha; [novas tentativas de webhook](https://developers.woovi.com/en/docs/webhook/webhook-retry) documenta 8 tentativas e repetição em HTTP >400 ou indisponibilidade. A semântica de consulta e estado das cobranças e as garantias de idempotência do PSP ainda precisam de verificação. Nenhuma chamada à API de produção ou sandbox foi realizada.

## Verificações de desenvolvimento

```sh
cd packages/woovi_pix_flutter && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test
cd ../../example && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test && mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true && mise exec -- flutter build apk --debug
cd ../examples/backend && mise exec -- gofmt -l . && mise exec -- go test -count=1 ./... && mise exec -- go build ./... && mise exec -- go vet ./...
```

Execute os testes de integração do servidor com o serviço PostgreSQL real, definindo a mesma `DATABASE_URL` antes de `go test -count=1 ./...`. Sem essa variável, os testes que dependem do PostgreSQL são pulados.

Consulte `CHANGELOG.md` e `LICENSE` para o histórico e os termos do projeto.
