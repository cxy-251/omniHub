#pragma once
#include <QString>
#include <QUrl>
#include <optional>

// Normalize a local file URL or preserve a media address, checking bounds before
// and after decoding. No filesystem access, network request or shell expansion.
inline std::optional<QString> mediaAddress(const QString &input) {
    if (input.isEmpty() || input.size() > 8192 || input.contains(QChar::Null)) return {};
    const QUrl url(input);
    if (input.startsWith("file:", Qt::CaseInsensitive) && (!url.isValid() || !url.isLocalFile())) return {};
    const QString address = url.isLocalFile() ? url.toLocalFile() : input;
    if (address.isEmpty() || address.size() > 8192 || address.contains(QChar::Null)) return {};
    return address;
}
