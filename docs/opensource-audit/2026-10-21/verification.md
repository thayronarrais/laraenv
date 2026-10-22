# Validação da preparação — 2026-10-21

Este registro preserva a validação inicial. Contagens de dependências e avisos abaixo pertencem àquela base; o estado posterior está na [validação de runtimes e dependências](runtime-dependency-verification.md) e na [revisão principal atual](README.md).

Base: `3ddf5a6c8fec279aa2d4f0168a18e0e7807cfb18`, branch `codex/workspace-source-control`. Backend privado e dados existentes fora das alterações. Evidências brutas, relatórios independentes, logs e originais preservados estão em `build/verification/opensource-licensing-20261021/`, ignorado pelo Git. Esta documentação resume resultados publicáveis, sem copiar evidências operacionais privadas.

## Testes e análises

- Go **1.26.10**, `go test -p 2 ./... -count=1 -timeout=12m`: **42 pacotes com testes passaram**, incluindo fixtures de armazenamento, SSH, deploy e atualizador. O diretório LaraEnv foi temporário e isolado. `go vet ./...` passou.
- Frontend, Vitest **4.1.11**, `npm test -- --maxWorkers=1`: **771 testes em 88 arquivos passaram**. O reset explícito de um mock e a execução do script Monaco em Node corrigem incompatibilidades da suíte com Vitest 4; as asserções foram mantidas. Os testes HTTPS passaram sem alteração do produto.
- Ferramentas de screenshots, execução sequencial: **104 testes em 18 arquivos passaram**, com Chromium, Monaco e xterm reais sobre o frontend compilado. A fixture passou a reconhecer a listagem local de deploys. Um teste agora aguarda o repaint observável do xterm; a revisão confirmou buffer, instância e contagem de terminais preservados durante navegação. A comparação de PNG variou numa execução concorrente e passou no conjunto completo sequencial, sem relaxar igualdade de bytes.
- `npm run lint`: **0 erros, 43 avisos existentes**. TypeScript e validação dos quatro catálogos de idioma passaram.
- As quatro fixtures Node de cópia de avisos passaram, incluindo dados ausentes, saída obsoleta, arquivo inesperado e link simbólico pendente. Nenhuma escrita é feita no modo `--check`.
- As **26 fixtures de licenças/PHAR/RTF** passaram; junto às quatro de cópia, são 30 testes Node. O gerador e seu `--check` conferem **789 componentes, 305 textos exatos e um arquivo de fontes correspondente**.
- A consulta de licenças no frontend compilado passou no Chromium, com rede externa bloqueada. Os dois textos exibidos correspondem integralmente a LICENSE e THIRD_PARTY_NOTICES.txt; nenhum texto é buscado ao montar Configurações. Fechar com Escape devolve foco ao botão.

As fixtures de links e operações temporárias do Windows precisaram executar fora do sandbox restritivo, que bloqueava criação/rename de arquivos de teste. Nenhum servidor SSH, banco, conta de nuvem, PHAR ou runtime real foi executado.

## Builds e artefatos

O frontend de produção compilou; o aviso de chunk acima de 500 kB permanece. O executável de revisão foi compilado com Go 1.26.10 e tags `desktop,wv2runtime.browser,production`, alinhadas ao inventário. Os bytes completos de LICENSE, NOTICE, avisos e inventário estão incorporados ao executável. Seus 36 módulos e os 32 módulos do TUI Linux recompilado estão cobertos nas mesmas versões pelo inventário. Os binários não foram executados.

O empacotador final gerou um MSI, um ZIP portátil e o executável, com três sidecars SHA256 correspondentes, sob uma pasta nova de revisão. Os seis arquivos de payload do MSI foram extraídos com WiX e conferidos por hash; os oito arquivos do portátil também correspondem às entradas revistas, incluindo o código-fonte WiX. A tabela File do MSI contém executável, licença, NOTICE, avisos, inventário e fontes WiX. Os hashes das extensões WiX usadas pelo empacotador são conferidos contra a política dos pacotes oficiais. As saídas anteriores foram preservadas.

Os pacotes usam versão de MSI 0.5.7 somente como fixture local. O executável de revisão mantém versão interna de desenvolvimento; não é uma nova versão pública nem um candidato validado para instalação em produção. O NSIS recebeu avisos/fontes e deixou de embutir/executar o bootstrapper proprietário do WebView2; sua authoring foi revista, mas a compilação NSIS não foi executada porque `makensis` não está disponível.

Uma cópia dos arquivos do índice Git conferiu **327 digests de entradas e evidências**, sem divergências com o inventário; os 12 originais arquivados estão ausentes desse snapshot. Isso verifica atributos de fim de linha e preservação de bytes no checkout, sem substituir um build em máquina limpa. Espaços e quebras de linha dos textos legais de terceiros são preservados deliberadamente e excluídos da checagem de whitespace; os demais arquivos continuam sujeitos a ela.

## Limites

Os scans atuais e achados retidos estão na [revisão principal](README.md). Não foram executados instalação/upgrade completo em VM Windows limpa, CGO/race, build real do pgvector, nem redistribuição de runtimes baixados. A extração de um MSI para inspeção é leitura de seu conteúdo, sem instalar ou iniciar o aplicativo. Nenhum release, push, mudança de visibilidade ou reescrita de histórico foi realizado.
