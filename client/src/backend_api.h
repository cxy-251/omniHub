#pragma once

#include <QObject>
#include <QNetworkAccessManager>
#include <QNetworkRequest>
#include <QVariantMap>
#include <QVariantList>
#include <QStringList>

// BackendApi discovers the service through the user's protected local connection file.
class BackendApi : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool hasAdmin READ hasAdmin NOTIFY statusChanged)
    Q_PROPERTY(bool connected READ connected NOTIFY statusChanged)
    Q_PROPERTY(QString error READ error NOTIFY statusChanged)
    Q_PROPERTY(QVariantList libraries READ libraries NOTIFY librariesChanged)
    Q_PROPERTY(bool stateReady READ stateReady NOTIFY librariesChanged)
    Q_PROPERTY(QString stateDirectory READ stateDirectory NOTIFY librariesChanged)
    Q_PROPERTY(QString libraryError READ libraryError NOTIFY librariesChanged)
    Q_PROPERTY(bool libraryBusy READ libraryBusy NOTIFY librariesChanged)
    Q_PROPERTY(QVariantMap migrationPreview READ migrationPreview NOTIFY librariesChanged)
    Q_PROPERTY(QStringList categoryDirectories READ categoryDirectories NOTIFY categoryDirectoriesChanged)
    Q_PROPERTY(QStringList disabledCategories READ disabledCategories NOTIFY categoryDirectoriesChanged)
public:
    explicit BackendApi(QObject *parent = nullptr);
    bool hasAdmin() const { return m_hasAdmin; }
    bool connected() const { return m_connected; }
    QString error() const { return m_error; }
    QVariantList libraries() const { return m_libraries; }
    bool stateReady() const { return m_stateReady; }
    QString stateDirectory() const { return m_stateDirectory; }
    QString libraryError() const { return m_libraryError; }
    bool libraryBusy() const { return m_libraryBusy; }
    QVariantMap migrationPreview() const { return m_migrationPreview; }
    QStringList categoryDirectories() const { return m_categoryDirectories; }
    QStringList disabledCategories() const { return m_disabledCategories; }
    Q_INVOKABLE void refresh();
    Q_INVOKABLE void setupAdmin(const QString &username, const QString &password);
    // 管理入口使用本机保护凭证；busy 时拒绝重入，回复完成后释放请求对象。
    Q_INVOKABLE void refreshLibraries();
    Q_INVOKABLE void recoverLibraries();
    Q_INVOKABLE void addLibrary(const QString &path, bool readOnly);
    Q_INVOKABLE void removeLibrary(const QString &id);
    Q_INVOKABLE void previewPrimary(const QString &id);
    Q_INVOKABLE void confirmPrimary();
    Q_INVOKABLE void cancelMigration();
    // 保存启用分类，服务端会校验路径并保留停用分类的原文件。
    Q_INVOKABLE void saveCategories(const QStringList &directories);
    static QNetworkRequest localRequest(const QString &path);
    // Shared platform/configured directory for protected native client state.
    static QString configurationDirectory();
signals:
    void statusChanged();
    void librariesChanged();
    void categoryDirectoriesChanged();
private:
    void consumeStatus(class QNetworkReply *reply);
    void libraryRequest(const QString &path, const QVariantMap &body, bool get = false, bool preview = false);
    QNetworkAccessManager m_network;
    bool m_hasAdmin = false;
    bool m_connected = false;
    QString m_error;
    QVariantList m_libraries;
    QVariantMap m_migrationPreview;
    QString m_stateDirectory;
    QString m_libraryError;
    QString m_previewLibraryId;
    bool m_stateReady = false;
    bool m_libraryBusy = false;
    QStringList m_categoryDirectories;
    QStringList m_disabledCategories;
};
