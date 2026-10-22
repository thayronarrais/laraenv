# Área de projetos

A página **Projetos** reúne os sites locais em uma lista compacta e mostra as ferramentas do projeto selecionado em um painel de detalhes. Cada linha informa o nome, o caminho, a stack e as tags do projeto. Os botões da linha executam seus atalhos preferidos; selecionar a linha abre seus detalhes.

## Busca, tags e páginas

Use a busca para encontrar projetos pelo nome, pelo caminho ou pelas tags. O filtro **Tags de clientes** é a principal forma de agrupar os projetos e mostra a quantidade de projetos associada a cada tag. **Todas as tags** inclui todos os projetos; **Sem tags** mostra os que ainda não receberam uma tag. Também é possível buscar uma tag no seletor ou usar uma tag da lista como filtro.

A busca e o filtro de tag podem ser combinados. **Limpar filtros** volta à lista completa. Quando nenhum projeto corresponde aos filtros, a página informa que não há resultados.

A lista mostra 10 projetos por página por padrão, com opções de 20 ou 50 em **Projetos por página**. A paginação informa o intervalo visível, o total de resultados e a página atual. Ao mudar a busca, o filtro ou a quantidade por página, a lista ajusta a paginação. Se uma atualização ou exclusão reduzir o número de páginas, a página atual também é ajustada.

O painel acompanha apenas um projeto visível na página atual. Se uma mudança de filtro ou de página tirar o projeto selecionado da lista visível, a seleção é limpa. O mesmo acontece quando o projeto deixa de existir após uma atualização ou exclusão.

## Atalhos da lista e tema

Use **Personalizar ações** para escolher os dois atalhos exibidos em cada linha: editor, navegador, terminal ou pasta. Se preferir apenas um atalho, ou nenhum, escolha **Oculto** nos espaços que não deseja usar. A escolha vale para todos os projetos e fica salva neste dispositivo; não é uma preferência sincronizada na nuvem. O editor e o terminal respeitam suas preferências existentes de aplicativo e comandos personalizados.

Os atalhos aparecem na ordem escolhida. Escolher um atalho que já está no outro espaço troca suas posições. **Restaurar padrão** volta à combinação editor e navegador.

As cores da lista, do painel e do projeto selecionado acompanham o tema definido nas configurações do LaraEnv. Ao mudar o tema, a tela de Projetos usa as mesmas cores de superfície e destaque das outras telas.

## Painel do projeto

Selecione um projeto para ver suas configurações de runtime e ferramentas. O painel organiza as ações em **Acesso rápido**, **Desenvolvimento** (incluindo o worker de fila do Laravel), **Scripts do projeto**, runtimes, **Ferramentas do projeto** e **Informações do projeto**, conforme os recursos disponíveis. O caminho e o domínio têm controles para copiar seus valores.

As ações já disponíveis continuam no painel e nos seus menus:

- Abrir o navegador, a pasta, o editor, o terminal e os agentes, respeitando as preferências salvas e os seletores de editor e terminal.
- Usar comandos personalizados, ajustar os comandos padrão e escolher PHP e Node automaticamente ou por versão, com acesso aos Downloads quando necessário.
- Iniciar, parar e consultar os logs do servidor Dev, configurar seu comando e iniciar, parar ou reiniciar o worker de fila com argumentos próprios.
- Configurar tags, HTTPS e proxy, editar o virtual host, acessar cron e histórico e executar o [Site Doctor](site-doctor.md).
- Criar novos projetos e excluir projetos pelas ações existentes.

Em janelas largas, a lista e os detalhes aparecem lado a lado. Em janelas mais estreitas, como 900 ou 720 pixels de largura, os detalhes aparecem sobre a lista. Use **Fechar detalhes do projeto** ou **Escape** para fechar esse painel. O foco permanece no painel enquanto ele está aberto e retorna à lista ao fechar.

## Desenvolvimento, scripts e comandos

**Desenvolvimento** também está disponível para projetos que usam apenas `package.json`, mesmo sem um framework reconhecido. Use **Iniciar Dev** e **Parar Dev** para controlar o processo gerenciado pelo LaraEnv e **Logs** para acompanhar sua saída. **Configurar comando** permite ajustar o comando usado por esse processo, por exemplo, para escolher o script de desenvolvimento do projeto.

**Scripts do projeto** lê os scripts declarados no `package.json`. O seletor mostra `dev`, `watch`, `build` e nomes personalizados somente quando esses scripts existem no arquivo. Use **Atualizar scripts** depois de editar o `package.json` ou **Tentar novamente** caso a leitura falhe. Ao executar um script, o LaraEnv usa o gerenciador de pacotes resolvido para o projeto, como npm, pnpm, Yarn ou Bun.

O campo **Comando personalizado** permite executar outro comando na pasta do projeto. Tanto os scripts quanto os comandos personalizados abrem um novo terminal integrado, respeitando o shell e as versões de PHP e Node do projeto. A saída permanece no terminal quando uma tarefa, como `build`, termina. Para encerrar comandos contínuos, como `dev` e `watch`, use **Ctrl+C** nesse terminal.

Executar um script ou um comando personalizado não altera o comando Dev salvo. Essa execução também não adiciona argumentos de servidor, como `--port`, a tarefas como `build`.

## Filas do Laravel

Projetos Laravel mostram **Worker de fila**, com os controles para iniciar, parar e reiniciar o worker. O menu da fila permite configurar seus argumentos. Esses controles e os parâmetros salvos continuam disponíveis independentemente dos scripts Node do projeto.

## Serviços compartilhados e processos do projeto

O estado **Servidor web em execução** ou **Servidor web parado** informa o estado global do processo do servidor web. Os pools PHP também são compartilhados por versão e podem atender vários projetos. As configurações de PHP e Node continuam sendo específicas de cada projeto.

O servidor **Dev** e o worker de fila pertencem ao projeto selecionado. Seus controles de iniciar e parar operam esses processos específicos. Para verificar a configuração de um site, use **Diagnóstico**; os resultados de saúde do Site Doctor são apresentados no seu próprio modal.
