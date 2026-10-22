# Site Doctor

O Site Doctor verifica a configuração dos projetos cadastrados no LaraEnv e oferece correções para os problemas que consegue resolver. Ele usa os runtimes efetivos de cada projeto, incluindo as versões detectadas nos arquivos do checkout. A seleção de PHP e Node é descrita em [Detecção de runtimes](project-runtime-detection.md).

## Como usar

Na página **Projetos**, selecione uma linha e clique em **Diagnóstico** nas ferramentas do painel de detalhes para verificar apenas aquele site. Consulte a [área de projetos](projects-workspace.md) para conhecer a busca, os filtros de clientes e os atalhos personalizáveis.

O botão **Diagnosticar todos**, no topo da página, reúne os resultados de todos os projetos cadastrados, inclusive os que não aparecem no filtro atual da página **Projetos**. O modal mostra um resumo com a quantidade total de projetos, quantos precisam de atenção e quantos estão com tudo certo. A lista compacta apresenta primeiro os projetos que precisam de atenção.

Use os filtros **Todos os projetos**, **Precisam de atenção** e **Saudáveis** para escolher quais resultados exibir. A busca encontra projetos pelo nome ou pelo caminho e pode ser combinada com o filtro de saúde. A lista mostra 10 projetos por página por padrão; também é possível escolher 20 ou 50. A paginação informa o intervalo exibido e o total de projetos que correspondem à busca e ao filtro.

Abra um projeto da lista para ver suas verificações e correções. Apenas um projeto é detalhado por vez. **Voltar para todos os projetos** retorna à lista preservando a busca, o filtro, a quantidade por página e a página selecionada. **Verificar novamente** no detalhe atualiza apenas aquele projeto e as contagens do resumo, sem executar novamente o diagnóstico de todos.

Cada verificação mostra o estado, a gravidade e uma explicação. **Tudo certo** indica que a verificação passou; **Falhou** ou **Atenção** apontam um problema identificado; **Opcional** indica uma sugestão que pode ser dispensada conforme o uso do projeto. **Não verificado** indica que a verificação não se aplica, depende de uma configuração anterior ou encontrou um limite da inspeção automática.

Um projeto marcado como **Tudo certo**, ou incluído no filtro **Saudáveis**, não tem falhas nem avisos nas verificações realizadas. Sugestões opcionais não prejudicam sua saúde. Esse estado não confirma verificações que ficaram como **Não verificado** nem garante o funcionamento completo do site. A lista também identifica projetos pausados e mostra as quantidades de sugestões opcionais e verificações não realizadas; as limitações dos projetos pausados continuam valendo.

Quando aparece **Revisão manual**, siga a orientação da verificação e execute o diagnóstico novamente. A ausência de correção automática não impede a identificação de um problema. Por exemplo, falhas de conexão ou autenticação do banco, extensões PHP ausentes e runtimes incompatíveis continuam exigindo atenção. Configurações dinâmicas ou em cache que não possam ser deduzidas, bancos externos ou não suportados, migrações sem sondagem disponível, runtimes do PATH fora do gerenciamento do LaraEnv e virtual hosts sem servidor configurado aparecem como **Não verificado**: são limites da inspeção, não falhas confirmadas do site.

Clique em **Corrigir** na verificação desejada. As correções são individuais; o diagnóstico de todos os sites não executa correções em lote. Após uma correção terminar, o Doctor verifica novamente o projeto afetado e mantém os resultados dos demais sites.

## Verificações e correções

| Verificação | Comportamento |
| --- | --- |
| Raiz pública do site | Verifica a existência da pasta que deve servir o site. |
| PHP e Node | Verifica a resolução do runtime e sua compatibilidade com o projeto. Quando falta uma versão, oferece acesso aos Downloads. |
| Dependências do Composer | Verifica `vendor/autoload.php` e oferece a instalação com o PHP efetivo do projeto quando os pré-requisitos estão disponíveis. |
| Dependências do Node | Verifica a instalação das dependências e usa o gerenciador de pacotes resolvido para o projeto. |
| Arquivo `.env` do Laravel | Quando falta, oferece a cópia de `.env.example`. Um arquivo existente é preservado. |
| Chave da aplicação Laravel | Verifica `APP_KEY` e oferece a geração de uma chave quando ela está ausente e os pré-requisitos estão prontos. |
| Link do storage Laravel | Verificação opcional de `public/storage`. Oferece **Criar link** quando o caminho está ausente e os pré-requisitos estão prontos. Links simbólicos e junctions do Windows para o destino padrão são aceitos. Um caminho ocupado ou um destino diferente é preservado. |
| Configuração do WordPress | Verifica a presença e a leitura de `wp-config.php`; a configuração ausente precisa ser criada manualmente. |
| Banco de dados | Verifica a conexão configurada e oferece a criação de um banco local ausente nos casos suportados. |
| Migrações Laravel | Verifica se há migrações pendentes e oferece sua execução após confirmação explícita. |
| Virtual host | Compara a configuração do site com a configuração gerada pelo LaraEnv e oferece a regeneração. Substituir uma configuração personalizada exige confirmação. |
| Arquivo hosts | Verifica as entradas necessárias para os nomes locais do site e oferece sua atualização pelo mecanismo existente do LaraEnv. |
| PHP-FPM | Verifica os serviços necessários para servir o site e oferece a inicialização dos que estão parados. |

Uma correção só aparece quando o Doctor confirma que seus pré-requisitos estão disponíveis. Por exemplo, a geração da chave e o link do storage dependem do PHP e das dependências do Laravel. Ao corrigir um pré-requisito, as próximas ações podem ficar disponíveis no novo diagnóstico.

O link `public/storage` só é necessário para servir arquivos do disco público local padrão do Laravel, armazenados em `storage/app/public`. Se o projeto não usa esse disco, pode deixar a sugestão opcional como está. **Criar link** executa `storage:link` como uma ação opcional; não é uma correção obrigatória para o projeto ser considerado saudável.

Se `public/storage` já for uma pasta, um arquivo ou um link para outro destino, o Doctor não remove nem substitui esse caminho. A revisão manual só é necessária se o projeto precisar do disco público local padrão. A impossibilidade de inspecionar esse link também é apresentada como opcional, com a mesma orientação de uso.

## Banco de dados e migrações

O Doctor preserva as credenciais de banco informadas pelo projeto. A criação automática é limitada a MySQL e PostgreSQL locais, em `localhost`, `127.0.0.1` ou `::1`, nas portas padrão `3306` e `5432`, respectivamente. O usuário configurado precisa ter permissão para criar o banco; o LaraEnv não troca essas credenciais por um usuário administrativo.

Conexões externas, portas diferentes, nomes de banco não suportados e configurações dinâmicas exigem revisão manual. A verificação de SQLite identifica o arquivo configurado, sem oferecer criação automática.

Uma porta inválida recebe um aviso de configuração. Defina um número inteiro de 1 a 65535 na configuração de banco do projeto e execute o diagnóstico novamente. Uma porta válida fora do padrão continua sendo um limite da inspeção automática.

A sondagem usa o PHP efetivo do projeto e os valores literais da configuração local, sem carregar a aplicação. O resultado confirma apenas essa conexão: a aplicação pode personalizar suas opções ou usar outra configuração. Falhas reconhecidas de transporte, autenticação e banco ausente continuam exigindo atenção. Um erro que o Doctor não consiga classificar, uma sondagem indisponível ou um resultado inválido ficam como **Não verificado**, sem prejudicar a saúde do projeto nem liberar migrações ou criação de banco. Erros de cliente, TLS ou da consulta não são apresentados automaticamente como servidor parado.

No Laravel, a presença de `bootstrap/cache/config.php` impede que o Doctor deduza a conexão a partir de `.env`: o cache pode conter outra configuração. A geração automática da chave da aplicação também fica indisponível quando há configuração em cache. Valores de banco que dependem de interpolação ou execução também não são inferidos. No WordPress, a leitura automática aceita declarações literais reconhecidas em `wp-config.php`; configurações calculadas ou carregadas de outros arquivos exigem revisão manual. O Doctor não executa esses arquivos para descobrir as credenciais.

Migrações podem alterar a estrutura e os dados do banco. Por isso, **Corrigir Migrações do banco de dados** abre uma confirmação específica antes de executar as migrações pendentes. Cancelar essa confirmação deixa o banco intacto.

## Saída, cancelamento e Downloads

A saída das correções aparece no próprio modal durante a execução. O botão **Cancelar correção** solicita a interrupção da operação atual. O modal permanece aberto até o comando terminar; ações que já foram concluídas não são desfeitas pelo cancelamento. A saída continua disponível para conferir o resultado, inclusive quando o comando falha.

Após uma correção, a saída permanece aberta nos detalhes do projeto. Ao voltar para a visão geral, ela fica recolhida para manter espaço para a lista de projetos. Os logs são preservados e podem ser abertos novamente pelo controle **Saída da correção**.

A saída é limitada para manter a interface responsiva. Quando o limite é atingido, o Doctor avisa que está exibindo apenas parte da saída. Mensagens de configuração e sondagens de banco são resumidas; o relatório não exibe o conteúdo de `.env` nem as credenciais do banco.

Quando falta um runtime compatível, **Abrir downloads de PHP/Node** leva à página de Downloads, filtrada para o runtime e, quando disponível, para a versão sugerida. Abrir essa página não instala nada. Depois da instalação, execute o diagnóstico novamente.

Um runtime encontrado no PATH do sistema fica como **Não verificado** porque sua versão não é gerenciada pelo LaraEnv. Quando o comando necessário está comprovadamente ausente do PATH, o Doctor aponta uma falha: selecione uma versão instalada de PHP ou Node para o projeto ou instale uma em **Downloads**, depois repita o diagnóstico.

Projetos pausados continuam com suas verificações de arquivos, mas não recebem correções. As verificações de serviços, rotas e banco que dependem de um projeto ativo ficam como **Não verificado**. Se um projeto não puder ser inspecionado durante o diagnóstico geral, ele permanece no relatório com um aviso e os demais resultados continuam disponíveis.
