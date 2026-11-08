/*
 * Tiny bridge between Go (wazero) and libopus.
 *
 * opus_encoder_ctl / opus_decoder_ctl are variadic, which a WebAssembly host
 * cannot call directly. These wrappers give fixed-arity entry points. Every
 * SET request in the Opus API takes one opus_int32 and every GET request one
 * opus_int32*, so two helpers per object cover all of them.
 */
#include <stdint.h>
#include "opus.h"

#define EXPORT(name) __attribute__((export_name(#name)))

EXPORT(yui_encoder_set)
int32_t yui_encoder_set(OpusEncoder *st, int32_t request, int32_t value) {
  return opus_encoder_ctl(st, request, (opus_int32)value);
}

EXPORT(yui_encoder_get)
int32_t yui_encoder_get(OpusEncoder *st, int32_t request, int32_t *out) {
  opus_int32 v = 0;
  int ret = opus_encoder_ctl(st, request, &v);
  *out = (int32_t)v;
  return ret;
}

EXPORT(yui_encoder_reset)
int32_t yui_encoder_reset(OpusEncoder *st) {
  return opus_encoder_ctl(st, OPUS_RESET_STATE);
}

EXPORT(yui_decoder_get)
int32_t yui_decoder_get(OpusDecoder *st, int32_t request, int32_t *out) {
  opus_int32 v = 0;
  int ret = opus_decoder_ctl(st, request, &v);
  *out = (int32_t)v;
  return ret;
}
