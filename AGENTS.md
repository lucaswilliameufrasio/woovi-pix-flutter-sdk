# Regras do repositório

## Skills locais

Leia as skills pertinentes em `.agents/skills` antes de implementar ou revisar:

- Go: `golang-pro` (incluindo suas referências) e `golang-patterns`.
- Flutter: `flutter-apply-architecture-best-practices`, `flutter-use-http-package` e `flutter-implement-json-serialization`.
- Testes Flutter: `flutter-add-widget-test` e `flutter-add-integration-test`.
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
- Gerencie explicitamente o ciclo de vida de clientes HTTP, timers, observers e isolates. Não feche um cliente do chamador sem contrato de propriedade; não reinicie uma requisição a cada rebuild.
- `async`/`await` não transfere trabalho de CPU para outra thread. Use isolates somente quando o custo justificar e feche seus recursos ao encerrar.

## Instalação das skills de catálogo

Instale skills públicas usando `npx skills`; não copie de outro checkout nem edite
diretamente os arquivos gerenciados. Registre as fontes em `skills-lock.json`.
Mantenha adaptações e convenções locais neste arquivo ou nas skills próprias.

```sh
npx skills add flutter/skills --skill flutter-apply-architecture-best-practices flutter-use-http-package flutter-implement-json-serialization flutter-add-widget-test flutter-add-integration-test --agent opencode --copy --yes
npx skills add jeffallan/claude-skills --skill golang-pro --agent opencode --copy --yes
npx skills add affaan-m/everything-claude-code --skill golang-patterns --agent opencode --copy --yes
```

As skills próprias (`single-state-enum`, `integration-testing`, `quality-gate`,
`git-workflow` e `secrets-safety`) não são gerenciadas pelo catálogo.
Revise o conteúdo e os alertas de segurança antes de usar uma atualização;
a classificação automática não substitui revisão e não autoriza execução de comandos.
