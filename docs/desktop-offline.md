# Desktop offline e nuvem opcional

O desktop usa os dados locais para projetos, serviços, runtimes, workspace, SSH, crons, presets e deployments. Conta, assinatura e validação pela API LaraEnv não autorizam nem bloqueiam esses recursos. O backend hospedado continua separado e privado.

## Nuvem

Em **Settings > Account**, ative a integração para acessar os serviços hospedados. Ela começa desativada em instalações novas e em perfis antigos que ainda não fizeram esse opt-in. Um token ou uma preferência antiga de sincronização não ativa a integração.

Desativar a nuvem conserva a URL, a sessão salva, a chave e a preferência de sincronização. Também conserva projetos, hosts, crons, presets, workspaces e deployments locais. Ações e planos do serviço hospedado ficam na área opcional da nuvem.

Desativar a integração ou trocar de conta não desfaz comandos já enviados por SSH. A operação conserva o host capturado; suas requisições e eventos da sessão antiga são cancelados, sem gravar resultados em outra conta. Confira o host antes de repetir uma operação que estava em andamento.

## Deployments

A página abre em **Local**. Crie um destino e selecione um host SSH salvo. Detectar, aplicar, fazer deploy e consultar atividade acessam esse host por SSH; configurações e histórico ficam no computador. O nome remoto salvo na configuração continua identificando o diretório e o log no servidor, mesmo que o cadastro tenha outro nome de exibição.

**Excluir o cadastro** remove somente o registro e seu histórico local. **Destruir** executa o plano de remoção no host remoto e exclui o cadastro após o sucesso. Se a remoção remota falhar, o cadastro e o resultado da tentativa permanecem disponíveis. Destinos sem uma configuração aplicada podem ser removidos pela ação de excluir cadastro.

Uma operação de deployment por vez pode modificar o servidor a partir deste processo desktop, incluindo os modos local e nuvem. A operação captura a configuração e a cadeia de hosts SSH antes de iniciar. Outros computadores, processos ou sessões TUI não participam desse bloqueio.

## Dados existentes e importação

Nenhum deployment hospedado é convertido ou executado automaticamente. Com a nuvem ativada e uma sessão válida, selecione o modo **Nuvem**, abra a importação de um destino e escolha explicitamente o host SSH local correspondente. A cópia recebe um UUID local e mantém a origem do serviço e da conta.

Também é possível importar um arquivo JSON ou colar seu conteúdo em **Local > Importar JSON**. Exemplo mínimo:

```json
{
  "version": 1,
  "source": "meu-arquivo-de-migracao",
  "deployments": [
    {
      "id": "42",
      "name": "Aplicação",
      "ssh_host_external_id": "host-antigo"
    }
  ],
  "host_mappings": {
    "host-antigo": "host-local"
  }
}
```

A interface solicita o mapeamento antes de confirmar. O identificador de origem deve ser uma string com UUID ou inteiro positivo; use a mesma origem nas importações seguintes. Repetir o mesmo conteúdo é idempotente. Conteúdo divergente gera um conflito e preserva a versão local. A importação nunca executa scripts nem abre SSH.

A API hospedada oferece resumos do histórico, mas não os corpos completos dos logs antigos. Esses resumos podem acompanhar a importação e aparecem sem um log local disponível. Arquivos de importação são fornecidos pelo usuário; este fluxo não cria um exportador de credenciais.

## Proteção e recuperação

Os novos dados ficam em `<raiz do LaraEnv>/state/deployments/`: índice versionado e logs separados, protegidos por DPAPI do Windows. A proteção independe da conta LaraEnv e da chave da nuvem. URLs persistidas não contêm o PAT; a credencial local fica no campo protegido. A interface e os logs de execução ocultam os segredos conhecidos.

Copiar a pasta para outro perfil Windows pode tornar esses dados indisponíveis. Nesse caso, o desktop preserva os envelopes originais e mostra os registros bloqueados. Recupere-os no perfil Windows original; não apague os arquivos para tentar desbloqueá-los. Para backup, feche o desktop e preserve a pasta de dados completa.

O histórico mantém as 100 execuções mais recentes de cada destino, com até 4 MiB por log e indicação de truncamento. Uma execução que estava ativa ao fechar o processo passa a aparecer como interrompida. Esse status descreve o histórico local: confira o servidor antes de repetir uma operação.

Se o comando remoto terminar e a gravação local falhar, a interface informa os dois resultados. O desktop não repete os comandos automaticamente.

## Conexões que continuam necessárias

O uso dos recursos já instalados não depende da API LaraEnv. Downloads de runtimes, Git remoto, SSH, emissão de certificados, atualizações e provedores externos continuam dependendo das redes e credenciais correspondentes.

Esta implementação prepara o desktop para a abertura do código. A escolha da licença, a revisão final de dependências e o processo de publicação do código continuam sendo etapas separadas.
