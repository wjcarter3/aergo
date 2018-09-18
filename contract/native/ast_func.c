/**
 * @file    ast_func.c
 * @copyright defined in aergo/LICENSE.txt
 */

#include "common.h"

#include "ast_func.h"

ast_func_t *
ast_func_new(char *name, modifier_t mod, list_t *param_l, list_t *return_l, 
             ast_stmt_t *blk, yylloc_t *lloc)
{
    ast_func_t *func = xmalloc(sizeof(ast_func_t));

    list_link_init(&func->link);
    func->lloc = *lloc;
    func->name = name;
    func->mod = mod;
    func->param_l = param_l;
    func->return_l = return_l;
    func->blk = blk;

    return func;
}

/* end of ast_func.c */
