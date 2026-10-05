# Usar o checkout Flutter

[Início](../../README.md) · [English](../en-us/flutter.md)

O pacote ainda não foi publicado no pub.dev. Use a dependência local do
[aplicativo de exemplo](../../example/pubspec.yaml) para experimentar.
A sessão vem do servidor do lojista; nunca crie cobranças diretamente na Woovi pelo app.

## Inicializar uma vez

Depois de receber uma `CheckoutSession` válida do servidor, crie os recursos
fora de `build` — por exemplo, em `initState` ou ao concluir a criação da sessão:

```dart
final transport = HttpCheckoutTransport(baseUrl: merchantBackendUrl);
final controller = CheckoutController(session: session, transport: transport);
```

Use `PixCheckoutView(controller: controller, onPaid: onPaid)` na interface.
O callback sinaliza um pagamento confirmado pelo servidor, mas **não autoriza
a entrega de um pedido**. A autorização comercial continua no backend.

## Observar o estado

`controller.state` é uma union imutável com o último estado de pagamento
confirmado pelo servidor:

| Estado de consulta | Conteúdo |
| --- | --- |
| `CheckoutReady` | Último `status` confirmado. |
| `CheckoutRefreshing` | Consulta em andamento, `status` anterior e contagem de falhas. |
| `CheckoutFailure` | `status` anterior, erro de transporte e contagem de falhas. |
| `CheckoutDisposed` | Controller encerrado; ignora respostas tardias. |

Os getters `status`, `transportError` e `isRefreshing` continuam disponíveis.
Erro de rede não vira estado de pagamento. `paid` não regride; `expired` pode
ser corrigido para `paid` por um Pix tardio confirmado pelo servidor.
O controller pausa consultas quando o app fica inativo e retoma ao voltar.

## Encerrar os recursos

O controller **não é dono do transporte**. Quando a tela/fluxo terminar:

```dart
controller.dispose();
transport.close();
```

- Sem `client` injetado, o transporte cria e fecha seu próprio cliente HTTP.
- Com `client` injetado, `transport.close()` não fecha o cliente do chamador.
  O dono deve fechá-lo quando nenhum consumidor precisar dele.
- `close()` é idempotente e impede novas consultas ou uso de respostas tardias.
- `requestTimeout` tem padrão de 10 segundos. Timeout não cancela, por si só,
  a operação HTTP subjacente; encerre os recursos ao abandonar o fluxo.
- Não recrie controllers/clientes a cada rebuild. A view não os descarta por você.

Veja também: [contrato do backend](backend.md) e [testes locais](development.md).
