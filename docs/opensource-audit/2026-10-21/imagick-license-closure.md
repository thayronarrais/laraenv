# Imagick — fechamento dos avisos de licença em 2026-10-21

Foram recuperados **69 textos, 673.343 bytes**, para suplementar exclusivamente o ZIP **Imagick 3.7.0 / PHP 8.3 NTS VS16 x64** identificado abaixo. O export conserva os avisos compostos, as alternativas de licença e os comentários originais que contêm permissões próprias. Não substitui todos os acompanhantes por uma licença ImageMagick, MIT ou GPL genérica.

Esta entrega fecha a **recuperação dos avisos nomeados para integração local**, incluindo o `bin/NOTICE` completo do SDK e os termos aninhados identificados. A integração e os testes de retenção no instalador pertencem à frente principal. Permanecem limites concretos para X11/Xext, o texto Adobe citado por LibRaw e a certificação de fonte/binário; eles são descritos adiante. Não é uma aprovação para espelhar o ZIP inteiro.

O modelo atual continua sendo o download opcional direto do fornecedor no computador do usuário. Este relatório não transforma esse download em redistribuição do runtime pelo projeto e não altera a licença do desktop ou o backend privado. A investigação foi estática: somente leitura de ZIPs, fontes, imports/exports PE, recursos, strings e hashes. Nenhuma DLL, EXE, runtime ou PHAR foi executado; nenhum serviço, backend ou credencial foi acessado. Nenhum código, policy, gerado ou asset público foi alterado nesta frente.

## Escopo verificável e arquivos de entrega

| Artefato primário | SHA256 |
| --- | --- |
| [ZIP PECL Imagick 3.7.0 / PHP 8.3](https://windows.php.net/downloads/pecl/releases/imagick/3.7.0/php_imagick-3.7.0-8.3-nts-vs16-x64.zip) | `4204071decbb06dce9f212a63ace0aa0c1b48fd344176b8274cd7d32ab674c71` |
| [SDK ImageMagick 7.1.0-18 VC15 x64](https://windows.php.net/downloads/pecl/deps/ImageMagick-7.1.0-18-vc15-x64.zip) | `3684a58b0896e2a55995029fa92cc13bd1ac778e03cdf8682c4369bbef86be9e` |
| `exportimagick-notices.json` desta entrega | `3cbd6374688620b3d6895e0e09ba915a935a3767cd9304673ef712ef4f96a41b` |
| `dll-license-matrix.json` desta entrega | `e3f676149ddb8ace58414637e77e9093a4cf6519c8d484092240cdb593e33753` |

As evidências ficam em `build/verification/redis-imagick-licenses-20261021/imagick-notices/`. O export contém um bundle vinculado ao **hash exato do ZIP**, extensão, versão e série PHP, com `path`, `bytes`, `sha256`, `sourceUrl`, `sourcePath`, `evidencePath` e descrição para cada texto. O escopo é `supplemental-notices-not-complete-binary-clearance`. Há **68 hashes de texto distintos** entre os 69 registros; a duplicação de um texto idêntico conserva sua origem por componente.

`exact-texts/<SHA256>.txt` guarda os bytes originais. Sete avisos que estavam em comentários de código foram recortados por posições de bytes, conservando os comentários completos: FreeType gzip/hash, PCRE, LibRaw DCB/X3F, Croco e Pango. Nesses registros, `sourceByteRange`, `sourceFileSha256` e `sourceFileEvidencePath` permitem reproduzir e conferir o recorte sem normalizar CRLF/LF, caracteres ou espaços. O fonte JPEG XL `color_management.cc` foi conservado inteiro para manter o contexto do aviso dos perfis ICC.

`delivery-validation.json` registra a conferência independente dos **69 hashes/tamanhos**, de todos os intervalos de fonte, do hash dos dois ZIPs e dos **166 matches binários**. Os scripts de coleta e verificação estão na mesma pasta. Downloads exploratórios indisponíveis e a primeira tentativa com rede restrita permanecem registrados; um 404 não foi tratado como prova de licença ausente.

A integração feita pela frente principal também foi conferida somente por leitura: `integration-review.json` compara os **69 arquivos incorporados** com o export final, incluindo bytes, hashes, tamanhos, URLs, descrições, membro/commit de origem e intervalos. Os **oito avisos anteriores e seus dois bundles** permanecem idênticos ao Git HEAD; o registry resultante contém 77 textos listados, sem arquivos TXT extras ou hashes de ZIP repetidos entre bundles. O manifest nesse snapshot tem SHA256 `0afbfee9f3e15a191b3d8483e59844ad13a524e4d7fd7543d6782b72201c265e`. Essa conferência de dados não substitui os testes de instalação executados pela frente principal.

## Matriz real das 166 DLLs acompanhantes

Foram reconferidas **166/166 DLLs idênticas byte a byte ao SDK**: **28 CORE_RL, 134 IM_MOD e quatro auxiliares**. `php_imagick.dll` é a extensão principal e fica fora da contagem dos acompanhantes. `dll-matrix-evidence.json` conserva hashes, recursos, imports, exports e strings observados; `dll-license-matrix.json` liga cada uma das 166 DLLs às famílias e arquivos de aviso, incluindo o percurso dos imports internos. Esse percurso não pretende descobrir todo código estático ou carregamento dinâmico possível.

As versões PE abaixo são as declarações do binário, corroboradas quando indicado por headers/fontes históricos. Os arquivos de fonte do Windows foram selecionados por commits imutáveis no corte de 2021-12-14 e comparados com os dados do SDK pela [frente de fontes/build](imagick-source-route.md). Um commit nesse corte é uma fonte histórica candidata, não uma prova automática de que gerou a DLL.

| Família/DLL | Quantidade | Versão observada e avisos recuperados |
| --- | ---: | --- |
| `CORE_RL_MagickCore_`, `MagickWand_`, `Magick++_` | 3 | ImageMagick 7.1.0-18; conservar LICENSE original e NOTICE do fonte. MagickCore inclui Raqm MIT e usa os demais acompanhantes por suas próprias licenças. As versões de recurso 10/5 são versões de interface, não novos releases ImageMagick. |
| `IM_MOD_RL_*` | 134 | Módulos ImageMagick: LICENSE e NOTICE próprios, mais os termos dos delegados efetivamente incorporados/importados. O nome de um módulo sozinho não prova inclusão de uma biblioteca homônima. |
| `FILTER_analyze_`, `ImageMagickObject` | 2 | Família ImageMagick, sob seus avisos e termos; também mapeados individualmente ao SDK. |
| `CORE_RL_bzlib_` | 1 | 1.0.8; licença bzip2 com copyright Julian Seward. |
| `CORE_RL_cairo_` | 1 | 1.16.0; **LGPL-2.1 OU MPL-1.1**, mantendo COPYING e os dois textos completos. Pixman MIT/X11 é incorporado separadamente. |
| `CORE_RL_exr_` | 1 | 3.1.3; avisos BSD de OpenEXR e Imath, LICENSE da árvore Windows e PATENTS. |
| `CORE_RL_flif_` | 1 | 0.3.0; implementação completa **LGPL-3.0-or-later**, com LGPL e GPL incorporada. Apache-2.0 é a alternativa para decoder-only, não uma permissão para o encoder observado. |
| `CORE_RL_freetype_` | 1 | 2.10.4; alternativa FTL preservada com crédito FreeType, termos BDF/PCF, hash e gzip/zlib. Não se elegeu a alternativa GPLv2 para todo o conjunto. |
| `CORE_RL_fribidi_` | 1 | 1.0.10; COPYING LGPL-2.1 e AUTHORS originais. |
| `CORE_RL_glib_` | 1 | 2.64.3; COPYING LGPL-2.1, AUTHORS, PCRE BSD e libffi MIT. |
| `CORE_RL_harfbuzz_` | 1 | 2.8.1; COPYING completo Old MIT, com sua coleção de copyrights. |
| `CORE_RL_jp2_` | 1 | JasPer 2.0.16; licença JasPer própria. É distinto de OpenJPEG. |
| `CORE_RL_jpeg-turbo_` | 1 | 2.1.1; LICENSE.md e README.ijg, incluindo crédito IJG, BSD e termos SIMD zlib. |
| `CORE_RL_jpeg-xl_` | 1 | 0.6.0; BSD-3-Clause, AUTHORS e PATENTS; Brotli MIT e Highway Apache-2.0. O aviso CC dos perfis ICC é separado do licenciamento do código. |
| `CORE_RL_lcms_` | 1 | 2.12; MIT, Marti Maria Saguer. |
| `CORE_RL_libheif_` | 1 | 1.12.0; LGPL-3.0. Incorpora libde265 LGPL-3.0 e AOM 3.1.3 BSD/patentes, com Fastfeat BSD e vector MIT selecionados pela configuração histórica. Não se inferiu x265 ou libwebm pelo suporte genérico de libheif. |
| `CORE_RL_liblzma_` | 1 | 5.2.5; COPYING distingue liblzma em domínio público de ferramentas/scripts GPL e código do toolchain. Não classificar a DLL como GPL apenas pelo pacote XZ. |
| `CORE_RL_libraw_` | 1 | Recurso 0.20.0.1, string 0.21.0-Snapshot202101; **LGPL-2.1 OU CDDL-1.0**, com COPYRIGHT e avisos BSD DCB/X3F. Ver limite Adobe abaixo. |
| `CORE_RL_librsvg_` | 1 | 2.40.20; LGPL histórico, GdkPixbuf e libcroco incorporados. Não confundir `GNU LIBRARY GENERAL PUBLIC LICENSE` com a GPL comum. |
| `CORE_RL_libxml_` | 1 | 2.9.10; Copyright MIT de libxml2. |
| `CORE_RL_libzip_` | 1 | 1.7.3; LICENSE BSD-3-Clause. |
| `CORE_RL_lqr_` | 1 | 0.4.2; **LGPL-3.0**, com COPYING.LESSER e a GPL incorporada conservadas juntas. |
| `CORE_RL_openjpeg_` | 1 | 2.3.1; coleção de copyrights BSD-2-Clause. |
| `CORE_RL_pango_` | 1 | Recurso 1.45.3, string 1.45.2; termos LGPL históricos e comentários originais. A divergência é reproduzida nos headers do fonte candidato, conforme abaixo. |
| `CORE_RL_png_` | 1 | 1.6.37; licença libpng completa, sem substituir seus copyrights históricos por MIT genérica. |
| `CORE_RL_tiff_` | 1 | 4.3.0; COPYRIGHT permissivo Sam Leffler/Silicon Graphics, conferido diretamente no tag oficial libtiff. |
| `CORE_RL_webp_` | 1 | 1.2.0; COPYING BSD e PATENTS. |
| `CORE_RL_zlib_` | 1 | 1.2.11; README original com licença zlib e copyrights Gailly/Adler. |
| `X11.dll`, `Xext.dll` | 2 | Match exato SDK/VisualMagick, mas fonte/licença dedicada desses binários ainda não certificada. Não atribuir uma licença de libX11 moderno por nome. |

O mapa também explicita as versões **candidatas de fonte** dos estáticos: Pixman 0.40.0, libffi 3.2.1, libcroco 0.6.13, libde265 1.0.8, AOM 3.1.3, Brotli 1.0.9, Highway 0.14.2 e Raqm 0.7.1. Elas não foram promovidas a versões binárias comprovadas apenas pelo corte temporal. Strings de Pixman, libde265 e paths de Highway/GdkPixbuf reforçam a identificação; as configurações históricas registram os projetos estáticos e os diretórios compilados.

## Pontos que exigiam distinguir termos e presença

**SDK NOTICE e NOTICE do fonte são documentos diferentes.** O `bin/NOTICE` tem **57.754 bytes**, SHA256 `20223c0bf2fa31beda1f640453360db73d6626c518a2a972053ff7356107983e`, e foi conservado integralmente, inclusive seus CR/CR/LF originais. Ele menciona hp2xx, ffmpeg e dcraw opcionais e inclui textos GPL/LGPL. Nenhum desses EXEs aparece no ZIP PECL examinado. Já o [NOTICE do tag ImageMagick 7.1.0-18](https://raw.githubusercontent.com/ImageMagick/ImageMagick/7.1.0-18/NOTICE), **12.016 bytes**, SHA256 `f23a0001567757e1fb7a2c588f511d277456233426582153a0567ba3cfba5c34`, contém avisos DuPont, OpenSSH, Xfig, ezXML, Magick++, Gsview e libsquish, entre outros. Um não substitui o outro.

**FLIF não é somente decoder.** A tabela de exports de `CORE_RL_flif_.dll` contém `flif_create_encoder`, `flif_encoder_encode_file` e `flif_encoder_encode_memory`, além do decoder. O [LICENSE do release 0.3](https://raw.githubusercontent.com/FLIF-hub/FLIF/v0.3/LICENSE) distingue expressamente a implementação LGPLv3+ do decoder Apache. Os quatro documentos originais foram preservados; conservar Apache sozinho seria insuficiente para essa DLL.

**LibRaw: a diferença de versão tem explicação no fonte.** Na [árvore histórica 944d5ed7758f04a6b11419b510dabc3d206f58e4](https://codeload.github.com/ImageMagick/libraw/zip/944d5ed7758f04a6b11419b510dabc3d206f58e4), `ImageMagick/ImageMagick.version.h` declara 0.20.0.1, enquanto `libraw/libraw_version.h` declara 0.21.0-Snapshot202101. Isso reproduz a combinação observada no binário. COPYRIGHT oferece LGPL-2.1 ou CDDL-1.0, declara que não usa código RESTRICTED de dcraw e identifica DCB/FBDD BSD e X3F BSD. Os comentários completos dos dois últimos foram recuperados. A menção genérica a dcraw GPL no NOTICE de programas opcionais não substitui esses termos da LibRaw concreta.

**Pango: headers gerados antigos.** Na [árvore histórica 30df8db265a687ba463ca1f42b25ba924994c905](https://codeload.github.com/ImageMagick/pango/zip/30df8db265a687ba463ca1f42b25ba924994c905), o recurso e meson dizem 1.45.3, mas `pango/config.h` e `pango/pango-features.h` ainda declaram MICRO 2 e `PANGO_VERSION_STRING "1.45.2"`. `pango-utils.c` usa essa macro. Os COPYING oficiais de 1.45.2 e 1.45.3 são byte-idênticos, **25.292 bytes**, SHA256 `d245807f90032872d1438d741ed21e2490e1175dc8aa3afa5ddb6c8e529b58e5`. Meson declara LGPL-2.1-or-later; os avisos originais de fonte e os textos LGPL são mantidos, sem reescrever o COPYING antigo. Croco também conserva um COPYING.LIB antigo, enquanto `cr-statement.c` especifica LGPL-2.1: ambos os documentos originais e o texto LGPL-2.1 do bundle permanecem disponíveis.

**Perfis ICC de JPEG XL têm aviso próprio.** A DLL contém literalmente o copyright Google 2019/CC-BY-SA-3.0 usado em [color_management.cc do release 0.6.0](https://raw.githubusercontent.com/libjxl/libjxl/v0.6.0/lib/jxl/color_management.cc). Foram preservados esse fonte completo e o [legal code CC-BY-SA-3.0](https://creativecommons.org/licenses/by-sa/3.0/legalcode.txt), para os perfis gerados. O header do código continua BSD; o aviso de dados/perfil não transforma a DLL inteira, o desktop ou o backend em CC-BY-SA.

O SDK e a string de delegates não indicam JBIG, DjVu ou FlashPIX habilitados; a existência dos módulos homônimos não demonstra a incorporação das bibliotecas GPL opcionais. Da mesma forma, `gslib`/`ps` e rotas de carregamento de Ghostscript não provam que um runtime Ghostscript esteja dentro desse ZIP. A matriz registra presença real dos arquivos e imports, sem converter capacidade opcional em uma licença global.

## Limites restantes e ação conservadora de integração

1. **Aplicar somente ao ZIP de hash exato acima**, preservando separadamente os documentos originais do PECL (`LICENSE`, `LICENSE.ImageMagick`, `tests/OFL.txt` e materiais de configuração). Acrescentar os 69 textos sem normalização ou sobrescrita dos originais. Registrar URLs, membro/intervalo de origem, hashes e o escopo descritivo do suplemento. O export não deve virar um `declaredLicense` único ou aprovação genérica para outros patches/séries PHP.
2. **X11/Xext permanecem com limite explícito.** Os mesmos bytes já estavam pré-compilados em `VisualMagick/bin`; X11 contém caminhos `/usr/X11R6.3/`. Isso orienta a busca, mas não identifica todas as modificações/avisos daquela compilação Windows. Não foi obtido um texto dedicado que se possa certificar como suficiente para esses dois hashes. A recuperação dos demais avisos não deve ser apresentada como encerramento dessa lacuna antes de mirror/embutimento.
3. **LibRaw/Adobe:** COPYRIGHT afirma que há trechos Adobe DNG SDK 1.4 sob MIT e esse crédito foi preservado integralmente. Não foi localizado um grant Adobe separado nos arquivos históricos examinados. Não se fabricou uma licença MIT com um copyright presumido nem se aplicou uma EULA de outro SDK à DLL. A correspondência desses trechos e eventual documento adicional continua identificada para uma redistribuição concreta.
4. **Fonte/build/relinking:** os textos recuperados não satisfazem sozinhos obrigações de fonte correspondente ou de substituição/relinking de bibliotecas LGPL. A [rota de fontes](imagick-source-route.md) separa commits, receitas, patches, dependências estáticas e os limites da correspondência. Antes de um mirror, MSI ou ZIP que efetivamente redistribua essas DLLs, concluir essa rota para o conjunto real e os termos escolhidos. Isso não exige publicar fontes de um backend privado que esta investigação não acessou nem vinculou às DLLs.
5. **Pré-requisitos externos continuam separados.** Imports de CRT/OpenMP/Windows e eventual Ghostscript obtido fora do ZIP não recebem licença através de LICENSE.ImageMagick. Esta entrega não contém esses redistribuíveis nem certifica suas condições.

Assim, há um conjunto concreto e verificável para corrigir a perda local dos avisos examinados no fluxo atual de download direto. A conclusão é limitada à retenção desses documentos para o hash revisado; não declara autenticidade do publisher, rebuild reprodutível, inventário estático completo de todo código possível ou liberação do pacote inteiro para redistribuição.
