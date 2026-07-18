import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  RegistryCoverage,
  RegistryImport,
  RegistryImportDecision,
  RegistryImportRequest,
  RegistryImportResult,
} from '../types/deployment-registry';

const baseUrl = '/api/v1/deployment-registry';

@Injectable({providedIn: 'root'})
export class DeploymentRegistryService {
  private readonly httpClient = inject(HttpClient);

  preview(request: RegistryImportRequest): Observable<RegistryImport> {
    return this.httpClient.post<RegistryImport>(`${baseUrl}/imports/preview`, request);
  }

  saveDecision(importId: string, decision: RegistryImportDecision): Observable<void> {
    return this.httpClient.post<void>(`${baseUrl}/imports/${importId}/decisions`, decision);
  }

  apply(importId: string, previewChecksum: string): Observable<RegistryImportResult> {
    return this.httpClient.post<RegistryImportResult>(`${baseUrl}/imports/${importId}/apply`, {previewChecksum});
  }

  get(importId: string): Observable<RegistryImport> {
    return this.httpClient.get<RegistryImport>(`${baseUrl}/imports/${importId}`);
  }

  coverage(importId: string): Observable<RegistryCoverage> {
    return this.httpClient.get<RegistryCoverage>(`${baseUrl}/coverage`, {params: {importId}});
  }
}
