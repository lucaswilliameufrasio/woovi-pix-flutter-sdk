# Desenvolvimento e verificações

[Início](../../README.md) · [English](../en-us/development.md)

Execute cada bloco abaixo em um terminal na **raiz do repositório**.
Instale as versões fixadas antes de começar:

```sh
mise install
```

## Pacote Flutter

```sh
(
  set -e
  cd packages/woovi_pix_flutter
  mise exec -- flutter pub get
  mise exec -- dart format --output=none --set-exit-if-changed lib test
  mise exec -- flutter analyze
  mise exec -- flutter test
)
```

## Aplicativo de exemplo

```sh
(
  set -e
  cd example
  mise exec -- flutter pub get
  mise exec -- dart format --output=none --set-exit-if-changed lib test
  mise exec -- flutter analyze
  mise exec -- flutter test
  mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true
  mise exec -- flutter build apk --debug
)
```

O modo sandbox dos testes usa configuração de teste, sem chamadas à Woovi.
O build iOS requer macOS/Xcode e não foi validado neste ambiente.

## Backend com PostgreSQL real

Não omita `DATABASE_URL`: sem ela, os testes de integração são pulados.
Use um banco isolado, não um banco com pagamentos reais ou tentativas incertas.

```sh
(
  set -e
  cd examples/backend
  docker compose -p woovi-pix-flutter-sdk-checks up -d --wait postgres
  PGPORT="$(docker compose -p woovi-pix-flutter-sdk-checks port postgres 5432 | sed 's/.*://')"
  export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
  test -z "$(mise exec -- gofmt -l .)"
  mise exec -- go vet ./...
  mise exec -- go test -race -count=3 ./...
  mise exec -- go build ./...
)
```

`test -z` faz a verificação falhar se o gofmt encontrar arquivos fora do padrão.
Os testes usam PostgreSQL e servidor HTTP local de teste para o PSP, não a API externa.
Três execuções no mesmo banco ajudam a detectar problemas de isolamento e concorrência.

Para o lint adicional, com `golangci-lint` instalado e compatível com a versão de Go:

```sh
(
  cd examples/backend
  mise exec -- golangci-lint run ./...
)
```

## Corrigir a formatação

As verificações acima não alteram arquivos. Para aplicar a formatação:

```sh
mise exec -- dart format packages/woovi_pix_flutter/lib packages/woovi_pix_flutter/test example/lib example/test
mise exec -- gofmt -w examples/backend
git diff --check
```

## Parar o banco de testes

```sh
(
  cd examples/backend
  docker compose -p woovi-pix-flutter-sdk-checks stop postgres
)
```

Os dados permanecem no volume; não o apague sem confirmar que pode ser descartado.

## Critério de entrega

- Formatação, lint, testes e builds precisam passar antes do commit.
- Testes de integração pulados não contam como validação.
- Checks locais não substituem validação de iOS ou do sandbox real.
- O repositório ainda não tem workflow de CI; estas verificações são manuais.
- As convenções e skills locais estão em [AGENTS.md](../../AGENTS.md).
