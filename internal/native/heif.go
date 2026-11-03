package native

/*
#cgo pkg-config: libheif
#include <libheif/heif.h>
#include <string.h>

static const char *image_transform_heif_mime(const uint8_t *data, int length) {
	const char *mime = heif_get_file_mime_type(data, length);
	if (strstr(mime, "-sequence") != NULL) {
		return mime;
	}
	// A still major brand can coexist with a compatible sequence brand.
	const char *sequences[] = {"avis", "msf1", "hevc", "hevx", "hevm", "hevs", "avcs"};
	for (unsigned int i = 0; i < sizeof(sequences) / sizeof(sequences[0]); i++) {
		if (heif_has_compatible_brand(data, length, sequences[i]) == 1) {
			return "image/heif-sequence";
		}
	}
	if (heif_has_compatible_brand(data, length, "avif") == 1) {
		return "image/avif";
	}
	return mime;
}
*/
import "C"

import "unsafe"

func heifMIME(header []byte) string {
	return C.GoString(C.image_transform_heif_mime((*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(header))), C.int(len(header))))
}
