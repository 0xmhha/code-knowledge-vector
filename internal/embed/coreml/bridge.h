#ifndef CKV_COREML_BRIDGE_H
#define CKV_COREML_BRIDGE_H

// Load a CoreML model from the given path (.mlpackage or .mlmodelc).
// Returns 0 on success, non-zero on failure.
int ckv_coreml_load(const char* model_path);

// Unload the currently loaded CoreML model and free resources.
void ckv_coreml_unload(void);

#endif
