# Regras do repositório

## Skills locais

Leia as skills pertinentes em `.agents/skills` antes de implementar ou revisar:

- Go: `golang-pro` (incluindo suas referências) e `golang-patterns`.
- Flutter: `flutter-architecting-apps`, `flutter-handling-http-and-json`, `flutter-handling-concurrency` e `flutter-testing-apps`.
- Estado de interface: `single-state-enum`.
- Testes e entrega: `integration-testing`, `quality-gate` e `git-workflow`.
- Credenciais e arquivos públicos: `secrets-safety`.

## Convenções explícitas

- Não use `if` de uma linha nem omita o bloco de uma instrução `if`, inclusive em testes. Condicionais de coleção do Dart não são instruções e seguem a sintaxe própria da linguagem.
- Modele cada fluxo com estados mutuamente exclusivos em um único enum ou union com payload. Não distribua o modo ativo entre flags de loading, error e dados anuláveis independentes. Condições realmente independentes não precisam ser fundidas em um único estado artificial.
- Mantenha `README.md` em pt-BR e `README-en-us.md` em inglês, com conteúdo técnico equivalente e links entre idiomas. Use o mesmo padrão nos exemplos.
- Não inclua nomes, caminhos privados, topologia ou regras de projetos de origem nos materiais reutilizados. Preserve atribuição e licenças públicas aplicáveis.
- Verifique versões atuais e compatibilidade antes de atualizar dependências; não force dependências transitivas além das restrições suportadas.
- Testes de integração usam PostgreSQL real e fixtures HTTP locais para a Woovi. Não faça chamadas externas de pagamento, deploy ou publicação sem autorização.
- Não declare uma implementação concluída com checks parciais ou testes de integração pulados. Siga o quality gate local e informe limitações de ambiente.
- Exemplos genéricos nas skills não substituem estas convenções nem instruções explícitas do usuário.
