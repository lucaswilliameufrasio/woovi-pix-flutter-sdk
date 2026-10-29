# Testar com a Woovi sandbox

[Início](../../README.md) · [English](../en-us/sandbox.md)

Use este guia depois de conhecer a [demonstração local](demo.md).
O onboarding e o comando de execução usam a CLI Go do exemplo.

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

## 2. Configurar e executar

Pré-requisitos: `mise`, Docker com Compose e um simulador/dispositivo Flutter
conectado. Na primeira execução, a CLI solicita o AppID sandbox sem exibi-lo,
gera os tokens de teste e salva tudo em `examples/backend/.env`. O arquivo tem
permissão `0600` em macOS/Linux e é ignorado pelo Git. Não o compartilhe.

```sh
mise install
cd examples/backend
mise exec -- go run ./cmd/woovi-demo
```

Esse único comando inicia PostgreSQL local, backend e Flutter. O onboarding
também pode ser executado separadamente com `mise exec -- go run
./cmd/woovi-demo setup`. O AppID fica apenas no processo Go; a CLI gera um
arquivo temporário protegido para passar o token de sessão ao Flutter, sem
colocá-lo no histórico do shell. O token ainda fica incorporado no app de teste:
não distribua esse build nem use esse mecanismo em produção.

Para um simulador iOS, escolha o destino explicitamente:

```sh
mise exec -- go run ./cmd/woovi-demo run --platform ios --device-id ID_DO_SIMULADOR
```

Para iniciar uma rodada nova depois que a anterior terminou, execute
`mise exec -- go run ./cmd/woovi-demo run --new-round`. **Não use `--new-round`
se o resultado anterior ainda estiver incerto**; nesse caso, reabra o app com a
mesma configuração com `mise exec -- go run ./cmd/woovi-demo run --resume` e
informe o pedido e a chave de idempotência exibidos no app. `Ctrl+C` encerra o app e o backend, e
para o contêiner PostgreSQL sem apagar o volume.

Toque **Pagar com Pix**: o app cria uma cobrança no sandbox. Para confirmá-la,
use a conta e o painel de teste Woovi; o app consulta o backend a cada 3 segundos.
O comando CLI apenas inicia serviços locais e não solicita pagamentos à Woovi.
Ele rejeita a configuração de PSP simulado e desabilita o worker independente;
ambiguidades são reconciliadas por GET.

### Console de teste servido pelo backend

Abra <http://127.0.0.1:8080/sandbox> no navegador. Informe o mesmo
`SANDBOX_SESSION_TOKEN`, o pedido da rodada atual e o
`SANDBOX_IDEMPOTENCY_KEY` usados pelo app. O console recupera o checkout com a
mesma chave e oferece:

- **Solicitar pagamento de sucesso na Woovi sandbox**: o backend consulta a
  cobrança e chama o endpoint oficial de pagamento de teste da Woovi com o
  `transactionID`; o AppID nunca passa pelo navegador. A resposta `paid` continua
  dependendo da consulta GET subsequente ao PSP.
- **Local: pago / expirado / expirado e depois pago**: respostas sintéticas,
  em memória, apenas para observar o comportamento do app. Não alteram Woovi nem
  o ledger PostgreSQL e desaparecem ao reiniciar o backend.

Uma cobrança sandbox paga é terminal e não pode ser paga de novo. Depois que o
app mostrar `paid` (ou `expired`), toque **Iniciar nova rodada sandbox**: o
backend cria um novo pedido de teste de R$ 25,99 e o app gera uma nova chave de
idempotência. O pedido e a chave da rodada atual aparecem na tela para permitir
recuperação após reiniciar o app; use-os em `SANDBOX_ORDER_ID` e
`SANDBOX_IDEMPOTENCY_KEY` se o resultado dessa rodada ainda estiver incerto.
Informe também o novo pedido no console. Não reutilize a chave de uma rodada paga.

O console não simula webhooks assinados nem estados internos do PSP. Use fixtures
HTTP locais para validar esses contratos. A documentação oficial descreve a
[operação de pagamento de teste](https://developers.woovi.com/en/docs/test-environment/test-account/test-pay-pix);
nenhuma chamada real ao endpoint sandbox foi feita durante o desenvolvimento.
Mantenha o backend acessível somente na máquina/rede privada de teste.

### Se aparecer `UNAUTHORIZED`

Na criação do checkout, esse 401 é emitido pelo próprio backend antes de chamar
a Woovi: o cabeçalho `X-Sandbox-Session` do app não corresponde ao token
configurado no processo Go (ou tem menos de 32 caracteres). Confirme que ambos
os terminais usam o mesmo token, que o app foi iniciado de novo após alterar o
`--dart-define`, e que a chamada está indo para o backend sandbox na porta
configurada em `DEMO_ADDR`. O AppID inválido normalmente só aparece depois, como
erro da integração com a Woovi, não como esse erro local de autorização.

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
