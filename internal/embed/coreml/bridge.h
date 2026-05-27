#ifndef CKV_COREML_BRIDGE_H
#define CKV_COREML_BRIDGE_H

#include <stdint.h>

// Load a CoreML model from the given path (.mlpackage or .mlmodelc).
// Returns 0 on success, non-zero on failure.
int ckv_coreml_load(const char* model_path);

// Unload the currently loaded CoreML model and free resources.
void ckv_coreml_unload(void);

// Run inference on tokenized input. input_ids and attention_mask are
// flat arrays of shape [batch_size * seq_len]. output is a pre-allocated
// buffer of shape [batch_size * seq_len * hidden_dim] that receives
// the last_hidden_state. Returns 0 on success.
int ckv_coreml_predict(
    const int64_t* input_ids,
    const int64_t* attention_mask,
    int batch_size,
    int seq_len,
    int hidden_dim,
    float* output
);

#endif
