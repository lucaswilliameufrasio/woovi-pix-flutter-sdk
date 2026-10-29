# Demonstração local

[Início](../../README.md) · [English](../en-us/demo.md)

Experimente o checkout sem conta Woovi ou integração de pagamento. O backend cria
um pedido fictício de R$ 25,99; a rota local de demonstração simula a confirmação.

> O QR é fictício: **não pague com um banco**. A rota de confirmação não tem
> autenticação e só deve ser habilitada nesta demonstração local.

## Antes de começar

- Instale [mise](https://mise.jdx.dev/getting-started.html) e Docker com Compose.
- Tenha um emulador Android disponível. Para iOS, use um Mac com Xcode.
- Execute os comandos em Bash, a partir da **raiz do repositório**.
- `mise install` instala as versões de Flutter e Go de `.mise.toml`. O Compose
  usa PostgreSQL 18 com imagem fixada por resumo criptográfico.

## 1. Iniciar o servidor

No primeiro terminal:

```sh
mise install
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo up -d --wait postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-demo port postgres 5432 | sed 's/.*://')"
export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
export ENABLE_DEMO_PSP=true
export DEMO_ADDR=127.0.0.1:8080
mise exec -- go run ./cmd/server
```

`DEMO_ADDR` define o endereço de escuta como `host:porta`; mantenha `127.0.0.1`
para que o backend aceite conexões somente deste computador. Se a porta `8080`
estiver ocupada, troque-a, por exemplo, para `127.0.0.1:9090`, e use `9090` na
URL do app e nos comandos `curl`. O Compose publica o PostgreSQL em uma porta
local dinâmica; o comando acima descobre essa porta e monta `DATABASE_URL`.
As credenciais do banco são públicas e exclusivas desta demonstração. As
migrações são aplicadas ao iniciar o servidor; o volume do Compose preserva os
dados entre execuções. Não habilite `ENABLE_WOOVI_WEBHOOK` junto com
`ENABLE_DEMO_PSP`.

## 2. Abrir o aplicativo

No segundo terminal, começando novamente na raiz:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Toque em **Pagar com Pix**. O app cria ou recupera o checkout de `demo-order-1`,
exibe o QR/código fictício e consulta o estado a cada 3 segundos. O QR não
representa um pagamento real.

| Onde o app roda | URL do servidor |
| --- | --- |
| Emulador Android | `http://10.0.2.2:8080` |
| Simulador iOS, servidor no mesmo Mac | `http://127.0.0.1:8080` |

No iOS, substitua a URL no comando. O uso de HTTP é apenas para desenvolvimento
local; iOS ainda não foi validado aqui. O guia cobre emuladores/simuladores; um
celular físico não alcança o backend limitado a `127.0.0.1` sem configuração
adicional de rede.

Se aparecer `UNAUTHORIZED`, confirme que o app está usando esta demo local, sem
`--dart-define=WOOVI_SANDBOX=true`; esse define troca a rota e a autenticação
para o fluxo sandbox. Na demo local, a consulta de status usa o bearer token do
checkout. Se ele estiver inválido, inicie o checkout novamente pelo app. Esse
erro não indica falha no QR.

## 3. Confirmar o pagamento simulado

Em outro terminal, crie/recupere a sessão do mesmo pedido e copie `checkout_id`
da resposta:

```sh
curl -sS http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Use o checkout_id retornado:
curl -X POST http://localhost:8080/v1/demo/checkouts/COLE_O_CHECKOUT_ID/pay
```

**Resultado esperado:** o app mostra pagamento simulado confirmado e oculta o
QR/código. A sessão pode rotacionar o bearer token; tokens anteriores continuam
válidos até expirar. A rota `/pay` não tem autenticação: mantenha o servidor
restrito a `127.0.0.1`, nunca o exponha à rede ou à internet. O callback do app
é apenas um sinal de interface, não uma autorização para entregar um pedido.

## Encerrar

Interrompa app e servidor com `Ctrl+C`. Para parar o PostgreSQL sem apagar o
volume persistente:

```sh
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo stop postgres
```

Execute o comando acima na raiz do repositório. Não remova volumes para
contornar tentativas incertas: os dados de demonstração são locais e persistem.

Próximo passo: [experimentar a Woovi sandbox](sandbox.md) ou [rodar os testes](development.md).
