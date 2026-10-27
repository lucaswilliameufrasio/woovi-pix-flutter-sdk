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

Você precisará de Docker Compose e mise. O projeto fixa Flutter **3.47.8**,
Go **1.27.3** e PostgreSQL **18.6**. O pacote declara suporte a Flutter 3.24+ / Dart 3.5+;
as verificações locais usam a versão fixada. `qr_flutter` 4.1.0 é a versão estável
mais recente publicada; `http` resolve para 1.6.0 no lockfile.
Precisão sobre QR: `qr_flutter` está na API v4, mas depende de `qr` 3.0.2;
o lockfile não usa `qr` 4.0 porque a restrição pública do `qr_flutter` é `^3.0.1`.
A versão do símbolo QR (1–40) é escolhida por `QrVersions.auto`, conforme o
payload; portanto, um payload curto pode de fato resultar em um símbolo v3.

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
