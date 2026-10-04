# Demonstração local

[Início](../../README.md) · [English](../en-us/demo.md)

Experimente o checkout sem conta Woovi e sem dinheiro real.
O servidor cria um pedido fictício; um comando local simula a confirmação do pagamento.

> O QR é fictício: **não pague com um banco**. A rota de confirmação não tem
> autenticação e só deve ser habilitada nesta demonstração local.

## Antes de começar

- Instale [mise](https://mise.jdx.dev/getting-started.html) e Docker com Compose.
- Tenha um emulador Android disponível. Para iOS, use um Mac com Xcode.
- Execute os comandos a partir da **raiz do repositório**, em Bash.
- `mise install` instala as versões de Flutter e Go de `.mise.toml`.
  O Compose usa PostgreSQL 18 com a imagem fixada por resumo criptográfico.

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

As credenciais acima são públicas e exclusivas do banco local de demonstração.
As migrações são aplicadas na inicialização do servidor.
Não habilite `ENABLE_WOOVI_WEBHOOK` junto com `ENABLE_DEMO_PSP`.

## 2. Abrir o aplicativo

No segundo terminal, começando novamente na raiz:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Toque em **Pagar com Pix**. O aplicativo deve exibir QR e código copiável
enquanto consulta o estado do checkout.

| Onde o app roda | URL do servidor |
| --- | --- |
| Emulador Android | `http://10.0.2.2:8080` |
| Simulador iOS, servidor no mesmo Mac | `http://127.0.0.1:8080` |

No iOS, substitua a URL no comando. O uso de HTTP é apenas para desenvolvimento
local; iOS ainda não foi validado aqui.

## 3. Confirmar o pagamento simulado

Em outro terminal, recupere o checkout do mesmo pedido:

```sh
curl -s http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Copie checkout_id da resposta:
curl -X POST http://localhost:8080/v1/demo/checkouts/COLE_O_CHECKOUT_ID/pay
```

**Resultado esperado:** o aplicativo mostra pagamento confirmado e oculta QR/código.
A recuperação pode rotacionar o token; os anteriores permanecem válidos até expirar.
O callback é apenas um sinal da interface, não uma autorização de entrega.

## Encerrar

Interrompa app e servidor com `Ctrl+C`. Para parar o banco sem apagar os dados:

```sh
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo stop postgres
```

O comando parte da raiz. Não remova volumes para contornar tentativas incertas.

Próximo passo: [experimentar a Woovi sandbox](sandbox.md) ou [rodar os testes](development.md).
