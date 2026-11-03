#ifndef AIR_REPLY_ARCHIVE_H
#define AIR_REPLY_ARCHIVE_H
#include <stddef.h>
#include <stdint.h>
int air_reply_inflate(const uint8_t *input, size_t input_size, uint8_t *output, size_t output_size);
uint32_t air_reply_crc32(const uint8_t *bytes, size_t size);
#endif
