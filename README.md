# Woovi Pix Flutter SDK

[English (en-US)](README-en-us.md)

**Checkout Pix para Flutter, com confirmação de pagamento no servidor.**
O pacote exibe o QR Code, permite copiar o código Pix e acompanha o estado do pagamento.

```text
Aplicativo Flutter → Servidor do lojista → Woovi
```

As credenciais Woovi ficam no servidor — nunca no aplicativo.

> **Projeto de exemplo, não pronto para produção.** Comece pelo pagamento simulado,
> sem dinheiro real. O lojista precisa implementar autenticação, validação de
> propriedade do pedido e liberação do pedido no servidor.

## O que você encontra aqui

| Diretório | Conteúdo |
| --- | --- |
| [`packages/woovi_pix_flutter`](packages/woovi_pix_flutter) | Pacote reutilizável: interface, controlador e transporte HTTP. |
| [`example`](example) | Aplicativo Flutter para experimentar o checkout. |
| [`examples/backend`](examples/backend) | Servidor Go, PostgreSQL e simulador de pagamentos. |

## Comece pela demonstração local

Não é necessário ter conta Woovi para experimentar o fluxo simulado.

1. Instale as ferramentas e **inicie o servidor**.
2. Abra o aplicativo no emulador e toque em **Pagar com Pix**.
3. Simule a confirmação e veja a interface atualizar.

**[Abrir o guia da demonstração →](docs/pt-br/demo.md)**

Você precisará de Docker Compose e mise. O projeto fixa Flutter **3.47.5**,
Go **1.27.1** e PostgreSQL **18**. O pacote declara suporte a Flutter 3.24+ / Dart 3.5+;
as verificações locais usam a versão fixada.

## Guias por objetivo

| Quero… | Guia |
| --- | --- |
| Executar e confirmar um pagamento fictício | [Demonstração local](docs/pt-br/demo.md) |
| Integrar a interface e gerenciar os recursos do SDK | [Checkout Flutter](docs/pt-br/flutter.md) |
| Criar uma cobrança na conta de teste Woovi | [Sandbox](docs/pt-br/sandbox.md) |
| Entender a API, idempotência e segurança | [Contrato do backend](docs/pt-br/backend.md) |
| Rodar formatação, lint, testes e builds | [Desenvolvimento](docs/pt-br/development.md) |

## Limites importantes

- **Não pague o QR de demonstração com banco real.**
- O callback do Flutter não autoriza a entrega de um pedido.
- O sandbox real e o iOS ainda não foram validados durante o desenvolvimento.
- O pacote não foi publicado no pub.dev; as integrações usam o código deste repositório.

[Histórico](CHANGELOG.md) · [Licença](LICENSE)
