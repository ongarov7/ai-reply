#include "AirReplyArchive.h"
#include <limits.h>
#include <string.h>
#include <zlib.h>

/* ZIP method 8 uses raw DEFLATE. Bound both input and output and require the
 * complete stream and expected size. CRC is independently checked by Swift. */
int air_reply_inflate(const uint8_t *input, size_t input_size, uint8_t *output, size_t output_size) {
    if (input_size > UINT_MAX || output_size > UINT_MAX) return 0;
    z_stream stream;
    memset(&stream, 0, sizeof(stream));
    stream.next_in = (Bytef *)input;
    stream.avail_in = (uInt)input_size;
    stream.next_out = output;
    stream.avail_out = (uInt)output_size;
    if (inflateInit2(&stream, -MAX_WBITS) != Z_OK) return 0;
    int result = inflate(&stream, Z_FINISH);
    int ok = result == Z_STREAM_END && stream.total_in == input_size && stream.total_out == output_size;
    inflateEnd(&stream);
    return ok;
}

uint32_t air_reply_crc32(const uint8_t *bytes, size_t size) {
    if (size > UINT_MAX) return 0;
    return (uint32_t)crc32(0L, bytes, (uInt)size);
}
