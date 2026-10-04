# Testar com a Woovi sandbox

[Início](../../README.md) · [English](../en-us/sandbox.md)

Use este guia depois de conhecer a [demonstração local](demo.md).
Os comandos partem da raiz do repositório e usam Bash.

> **Somente testes:** use uma conta e um banco isolados. Não exponha o servidor
> publicamente nem pague com um banco real. Nenhuma chamada ao sandbox foi
> validada durante o desenvolvimento.

## 1. Preparar a conta

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

## 2. Iniciar o servidor (terminal 1)

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

## 3. Iniciar o aplicativo (terminal 2)

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

## 4. Recuperar erros sem duplicar cobranças

- Após timeout ou `PAYMENT_PROCESSING`, tente novamente com **a mesma chave**.
  O servidor recupera por GET; não repete o POST ao PSP.
- Não apague o banco ou a tentativa para contornar resultados ambíguos.
- Use uma nova chave somente após `EXPIRED` confirmado no PSP.
  Um pedido pago não admite outra cobrança.
- Para uma rodada independente, use outro banco isolado e outro pedido/ambiente
  de teste, sem descartar uma tentativa ainda incerta.
- Erros de consulta não significam pagamento confirmado.

## iOS e dispositivos físicos

No simulador iOS, com backend no mesmo Mac, substitua a URL por
`http://127.0.0.1:8080` e execute `mise exec -- flutter run -d ID_DO_SIMULADOR ...`.
Veja IDs com `mise exec -- flutter devices`. iOS precisa de macOS/Xcode e não
foi validado aqui. No aparelho físico, use IP LAN do host e binding apropriado;
somente em rede privada de teste (HTTP não protege o token). Preferir simulador.

## Webhook opcional

Polling GET funciona sem túnel/webhook. Para testar webhook, configure no painel
sandbox uma URL HTTPS acessível para `/v1/webhooks/woovi` e no backend:

```sh
export ENABLE_WOOVI_WEBHOOK=true
export WOOVI_WEBHOOK_PUBLIC_KEYS_URL=https://api.woovi-sandbox.com/api/v1/webhook/public-keys
```

Reinicie o backend; nunca habilite `ENABLE_DEMO_PSP` junto. Não exponha a sessão
de teste via um túnel público irrestrito; exponha apenas o caminho de webhook.
O servidor verifica os webhooks, mas a liberação comercial do pedido continua
fora do exemplo. Os testes automatizados usam um servidor HTTP local, não a Woovi.

Veja também: [contrato e segurança do backend](backend.md).
